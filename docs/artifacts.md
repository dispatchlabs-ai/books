# File transfer and large MCP results

Configure an absolute `artifact_directory` in the private HTTP server configuration
or MCP policy to enable file transfer. Books creates it with mode 0700 and rejects
a symlink or a directory accessible to other users. Use a dedicated directory.
Clients cannot select this path. Files are bound to the authenticated actor and
verified database UUID; company files also bind the entity and book identity.
Changing an alias to another database does not transfer access to old files.

Both company and database operation routes and MCP tools expose these operations:

| Operation | Input | Result |
| --- | --- | --- |
| `artifact_begin` | Stable `key`, safe original `name`, exact byte `size`, lowercase `sha256` | Opaque reference and received byte count |
| `artifact_write` | `id`, exact byte `offset`, `base64` chunk | Updated received byte count |
| `artifact_finish` | `id` | Verified complete reference |
| `artifact_read` | `id`, exact byte `offset`, optional `length` | Base64 chunk and `eof` |
| `artifact_discard` | `id` | Discarded reference |

Use the same authenticated identity and scope for each call. Company upload calls
require `import`; database upload calls require `manage`. Reading and discarding
one's own scoped files require `read`. A company management grant does not expose
another principal's files. Discard affects temporary file storage, never retained
ledger evidence. It keeps a tombstone; a discarded key cannot be reused.

Files may be at most 256 MiB and chunks at most 256 KiB. Begin reserves the declared
size against a 1 GiB store-wide limit. At most 4096 metadata records, including
tombstones, are retained. Operators can retire an unused dedicated artifact
store after its references are no longer needed. Transfers append at the received
offset. Identical retries succeed; changed bytes conflict. An interrupted partial
chunk can be resent in full. Finish verifies the complete length and digest.
Storage locks honor request cancellation. Only complete verified files can be
consumed by an operation.

For `bank_import_upload`, send `artifact` instead of inline `base64`. The existing
8 MiB statement limit still applies. HTTP and MCP may resume the same upload when
they use the same artifact directory, actor, and verified scope. This transports
source bytes; parser validation and accounting preview/apply remain separate.

MCP normally returns `{ "result": ... }`. Results larger than 512 KiB are stored
as JSON and returned as `{ "artifact": ... }`; read their chunks and decode the
assembled JSON to obtain the same result. Each reference includes size and SHA-256.
The chunk-read result itself remains inline. Read-only agents can discard their
own generated results when finished.

If artifact delivery fails after a mutation succeeds, MCP returns the successful
result inline with `delivery_warning`, even if it exceeds the normal inline limit.
Do not repeat that mutation. This preserves its result rather than misreporting a
successful write as failed. Durable operation receipts for process/network loss
remain separate implementation work; current operations retain their existing
idempotency and retry contracts.

Bundles, maintenance backup streaming and explicit authorized local-path mappings
remain pending; this initial transfer layer does not yet imply full file parity.
