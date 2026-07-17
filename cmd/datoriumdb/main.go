package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JohnAD/datoriumdb/internal/agents/cache"
	"github.com/JohnAD/datoriumdb/internal/agents/change"
	"github.com/JohnAD/datoriumdb/internal/agents/upgrade"
	"github.com/JohnAD/datoriumdb/internal/auth"
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/engine"
	"github.com/JohnAD/datoriumdb/internal/establish"
	"github.com/JohnAD/datoriumdb/internal/replication"
	"github.com/JohnAD/datoriumdb/internal/scheduler"
	"github.com/JohnAD/datoriumdb/internal/server"
)

// Set at link time by release builds: -X main.version=v0.1.0
var version = "dev"

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: datoriumdb <serverName> <establishmentBaseURL> [--listen addr] [--config-dir path] [--data-dir path]\n")
		os.Exit(1)
	}
	serverName := os.Args[1]
	estURL := strings.TrimRight(os.Args[2], "/")
	if serverName == "" || estURL == "" {
		fmt.Fprintf(os.Stderr, "server name and establishment base URL are both required\n")
		os.Exit(1)
	}
	listen := "127.0.0.1:8080"
	configDir := "/db/.config"
	dataDir := "/db"
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--listen":
			i++
			listen = os.Args[i]
		case "--config-dir":
			i++
			configDir = os.Args[i]
		case "--data-dir":
			i++
			dataDir = os.Args[i]
		default:
			fmt.Fprintf(os.Stderr, "unknown arg %s\n", os.Args[i])
			os.Exit(1)
		}
	}

	bootstrapSecret := os.Getenv("DATORIUMDB_MACHINE_BOOTSTRAP_SECRET")
	signingKeyFile := os.Getenv("DATORIUMDB_SIGNING_KEY_FILE")

	eng := &engine.Engine{
		ConfigDir:  configDir,
		DataDir:    dataDir,
		ServerName: serverName,
	}

	// A server is the establishment server when its own name matches the
	// establishmentServer named by its already-present local config.
	// tech-docs/AUTHENTICATION.md's "Establishment Self-Start": the
	// establishment server loads /db/.config locally and never calls HTTP
	// /establish against itself.
	isEstablishmentSelf := false
	if cfg, err := config.Load(configDir); err == nil && cfg.General.General.EstablishmentServer == serverName {
		isEstablishmentSelf = true
		eng.Cfg = cfg
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var issuer *auth.Issuer
	var tokens replication.TokenSource
	if isEstablishmentSelf {
		if err := eng.Reload(); err != nil {
			log.Fatalf("load config: %v", err)
		}
		if signingKeyFile != "" {
			iss, err := auth.NewIssuerFromFile(eng.Cfg.Auth, signingKeyFile)
			if err != nil {
				log.Fatalf("load signing key from DATORIUMDB_SIGNING_KEY_FILE: %v", err)
			}
			issuer = iss
			tokens = replication.IssuerTokenSource{Issuer: issuer, ServerName: serverName}
		} else {
			log.Printf("warning: DATORIUMDB_SIGNING_KEY_FILE not set; this establishment server cannot issue machine tokens")
		}
	} else {
		if bootstrapSecret == "" {
			log.Fatalf("DATORIUMDB_MACHINE_BOOTSTRAP_SECRET is required for non-establishment servers")
		}
		worker := &establish.Worker{
			ServerName:       serverName,
			EstablishmentURL: estURL,
			BootstrapSecret:  bootstrapSecret,
			ConfigDir:        configDir,
			DataDir:          dataDir,
		}
		if err := worker.Bootstrap(ctx); err != nil {
			log.Fatalf("establishment bootstrap failed: %v", err)
		}
		if err := eng.Reload(); err != nil {
			log.Fatalf("load config after bootstrap: %v", err)
		}
		tokens = worker
		go worker.Run(ctx, func(err error) {
			log.Printf("establishment worker gave up after repeated failures, shutting down: %v", err)
			os.Exit(1)
		})
	}

	for collection := range eng.Cfg.Schemas {
		_ = os.MkdirAll(filepath.Join(dataDir, collection), 0o755)
	}

	// Wire replication (tech-docs/REPLICATION-FAILURE-HANDLING.md and
	// tech-docs/SERVER-TO-SERVER-API.md): SOT-side push-then-pending
	// delivery, plus a read/proxy-member catch-up loop and durable
	// SOT-restart recovery for interrupted operations.
	if tokens != nil {
		eng.Replicator = &replication.Coordinator{
			ServerName: serverName,
			DataDir:    dataDir,
			Cfg:        eng.Cfg,
			Tokens:     tokens,
		}
		if resumed, err := eng.Replicator.ResumeIncomplete(ctx); err != nil {
			log.Printf("warning: replication resume-on-startup encountered an error: %v", err)
		} else if len(resumed) > 0 {
			log.Printf("resumed replication for %d incomplete operation(s) found on startup", len(resumed))
		}

		eng.ReadState = &replication.ReadMemberState{
			StaleThreshold: eng.Cfg.General.General.ReadMemberFailedCheckinsBeforeStale,
		}
		agent := &replication.CatchUpAgent{
			ServerName: serverName,
			DataDir:    dataDir,
			Cfg:        eng.Cfg,
			Tokens:     tokens,
			State:      eng.ReadState,
		}
		checkinInterval := time.Duration(eng.Cfg.General.General.ReadMemberCheckinSeconds) * time.Second
		if checkinInterval <= 0 {
			checkinInterval = 10 * time.Second
		}
		go runCatchUpLoop(ctx, agent, eng, checkinInterval)
	}

	// Wire the in-process scheduler and background agents
	// (tech-docs/LOCAL-ARCHITECTURE.md): one change-agent worker draining
	// .changeQueue and distributing search/cache work, one upgrade-agent
	// worker migrating documents left behind by a schema upgrade, and (if
	// this server can authenticate outbound server-to-server calls) one
	// cache-agent worker pulling pending cache-update work from other
	// SOT-members. change-agent and upgrade-agent share one ExclusionSet
	// so they never edit the same document at the same time.
	cfgSource := func() *config.Config { return eng.Cfg }
	docExclusion := scheduler.NewExclusionSet()
	sched := scheduler.New(nil)

	localApplier := &change.LocalApplier{DataDir: dataDir}
	router := change.SearchRouter(localApplier)
	if tokens != nil {
		router = &change.ShardRouter{
			ServerName: serverName,
			Cfg:        cfgSource,
			Local:      localApplier,
			Remote:     &change.RemoteApplier{Cfg: cfgSource, Tokens: tokens},
		}
	}
	changeAgent := &change.Agent{
		DataDir:    dataDir,
		ServerName: serverName,
		Cfg:        cfgSource,
		Router:     router,
		Exclusion:  docExclusion,
		Logf:       log.Printf,
	}
	sched.Register(scheduler.Agent{
		Name:     "change-agent",
		Interval: 2 * time.Second,
		Task:     changeAgent.RunOnce,
	})

	upgradeAgent := &upgrade.Agent{
		DataDir:   dataDir,
		Cfg:       cfgSource,
		Exclusion: docExclusion,
		Logf:      log.Printf,
	}
	sched.Register(scheduler.Agent{
		Name:     "upgrade-agent",
		Interval: 5 * time.Second,
		Task:     upgradeAgent.RunOnce,
	})

	if tokens != nil {
		cacheAgent := &cache.Agent{
			ServerName: serverName,
			DataDir:    dataDir,
			Cfg:        cfgSource,
			Tokens:     tokens,
			Logf:       log.Printf,
		}
		cacheInterval := time.Duration(eng.Cfg.General.General.CacheUpdateCheckinSeconds) * time.Second
		if cacheInterval <= 0 {
			cacheInterval = 30 * time.Second
		}
		sched.Register(scheduler.Agent{
			Name:     "cache-agent",
			Interval: cacheInterval,
			Task:     cacheAgent.RunOnce,
		})
	}
	sched.Start(ctx)

	srv := &server.HTTPServer{
		Engine:          eng,
		Issuer:          issuer,
		BootstrapSecret: bootstrapSecret,
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("datoriumdb %s listening on %s (establishment %s)", serverName, listen, estURL)

	httpServer := &http.Server{Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// runCatchUpLoop implements the read/proxy-member side of
// tech-docs/REPLICATION-FAILURE-HANDLING.md's "Read-Member Catch-Up": every
// checkinInterval, check in with each SOT-member this server depends on for
// at least one shard slot, applying and completing any pending work.
func runCatchUpLoop(ctx context.Context, agent *replication.CatchUpAgent, eng *engine.Engine, checkinInterval time.Duration) {
	ticker := time.NewTicker(checkinInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for _, sot := range replication.RelevantSOTServers(eng.Cfg, agent.ServerName) {
			if err := agent.CheckIn(ctx, sot); err != nil {
				log.Printf("replication check-in with %s failed: %v", sot, err)
			}
		}
	}
}
