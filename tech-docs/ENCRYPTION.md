# Encryption

This document is a placeholder for future design work around client-driven encryption.

Encryption is not part of the MVP.

## Intent

The README mentions "Intrinsic Encryption". The current direction is that this will not primarily mean collection or sub-collection encryption.

Instead, encryption support is expected to focus on smart-client authorization and document-level key selection.

For example, in a multi-tenant system, each tenant could have unique encryption keys. Documents associated with that tenant would be encrypted with the tenant's keys.

## Smart Client Responsibility

Encryption is expected to be driven by smart clients.

A smart client may be responsible for:

- deciding whether a document should be encrypted
- selecting the correct key based on tenant, user, policy, or application context
- encrypting content before sending it to the database
- decrypting content after reading it from the database
- refusing to send or read data when authorization is missing

The database should not need to understand the tenant's authorization model in detail.

## Document-Level Isolation

The goal is that compromising one document's key should not automatically expose other documents.

This may imply document-level or tenant-document-level key separation.

The exact key hierarchy is still to be determined.

## Open Questions

- Which fields are encrypted: whole document, selected SOT fields, extra fields, cached summaries, or some combination?
- How does encryption interact with precompiled searches?
- How does encryption interact with cached summaries?
- How are keys identified without leaking sensitive tenant information?
- Should encrypted documents expose enough metadata for routing and schema validation?
- Can encrypted fields participate in schema upgrade operations?
- What should happen when a client cannot decrypt a referenced document?

## Non-Goals For Now

- No MVP encryption support.
- No finalized key hierarchy.
- No finalized tenant authorization model.
- No finalized encrypted search behavior.
