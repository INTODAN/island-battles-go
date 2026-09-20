# Legacy lobby cutover

Stop the old backend and back up PostgreSQL before starting the new backend.
Bootstrap first initializes the generic lobby schema, then calls `MigrateLegacy`,
then initializes current game and adapter tables. No player ownership is inferred.

The version-1 migration copies every old `players` row and each game's old room
name/timestamps into `lobby_legacy_records`, including soft-deleted records. Source
keys are `(source_table, source_primary_key)`. JSON is PostgreSQL's canonical
`jsonb::text` representation; SHA-256 is computed over those exact UTF-8 bytes,
so bigint IDs never pass through floating-point conversion. The report digest
hashes `table:key:row_checksum\n`, players before games and numeric ID ascending.

A PostgreSQL advisory transaction lock serializes startup. Archives, counts,
nullable `games.lobby_id`, nullable/default-free deprecated `room_name`, and the
completion marker commit together. Conflicting archives abort the transaction.
An identical completed rerun returns the saved report, without treating new games
as legacy rows. `VerifyArchive` checks stored JSON against its saved row digests.

Source rows, simulation tables, game IDs and foreign keys are retained. Legacy
games remain detached; archive records have no player API or claim mechanism.
New detached games omit deprecated room names (NULL permits multiple games under
the old unique index). Removing old columns/tables is a separate future migration.

After verification, start only the new backend. Once new detached games exist,
do not simply restart the old binary: its room-name/player assumptions differ.
Rollback requires a separately reviewed database restore/cutover plan. There is
no automatic destructive down migration.
