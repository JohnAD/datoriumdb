package ctl

import (
	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
)

func cmdGeneralSet(ctx *Context, args []string) Outcome {
	name, args, hasName := extractFlag(args, "--name")
	establishmentServer, args, hasEstablishment := extractFlag(args, "--establishment-server")
	readCheckin, args, err := extractIntFlag(args, "--read-member-checkin-seconds")
	if err != nil {
		return ValidationFailSimple("general.set", "invalidArguments", err.Error())
	}
	cacheCheckin, args, err := extractIntFlag(args, "--cache-update-checkin-seconds")
	if err != nil {
		return ValidationFailSimple("general.set", "invalidArguments", err.Error())
	}
	failedCheckins, _, err := extractIntFlag(args, "--read-member-failed-checkins-before-stale")
	if err != nil {
		return ValidationFailSimple("general.set", "invalidArguments", err.Error())
	}

	mutate := func(cfg *config.Config) (*Plan, map[string]any, []envelope.Error) {
		fields := map[string]any{}
		var errs []envelope.Error
		if hasName {
			if name == "" {
				errs = append(errs, envelope.Error{Code: "invalidArguments", Path: "/general/name", Message: "name must not be empty"})
			} else {
				cfg.General.General.Name = name
			}
		}
		if hasEstablishment {
			cfg.General.General.EstablishmentServer = establishmentServer
		}
		if readCheckin != nil {
			if *readCheckin <= 0 {
				errs = append(errs, envelope.Error{Code: "invalidArguments", Path: "/general/readMemberCheckinSeconds", Message: "must be a positive integer", Actual: *readCheckin})
			} else {
				cfg.General.General.ReadMemberCheckinSeconds = *readCheckin
			}
		}
		if cacheCheckin != nil {
			if *cacheCheckin <= 0 {
				errs = append(errs, envelope.Error{Code: "invalidArguments", Path: "/general/cacheUpdateCheckinSeconds", Message: "must be a positive integer", Actual: *cacheCheckin})
			} else {
				cfg.General.General.CacheUpdateCheckinSeconds = *cacheCheckin
			}
		}
		if failedCheckins != nil {
			if *failedCheckins <= 0 {
				errs = append(errs, envelope.Error{Code: "invalidArguments", Path: "/general/readMemberFailedCheckinsBeforeStale", Message: "must be a positive integer", Actual: *failedCheckins})
			} else {
				cfg.General.General.ReadMemberFailedCheckinsBeforeStale = *failedCheckins
			}
		}
		if len(errs) > 0 {
			return nil, fields, errs
		}
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}

		plan := &Plan{}
		nextVersion, err := stageGeneralBump(plan, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields["generalVersion"] = nextVersion
		return plan, fields, nil
	}
	return runMutation(ctx, "general.set", mutate)
}
