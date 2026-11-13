-- The lobby's own role and database on the local PostgreSQL, beside Island
-- Battles' (#229), as the VM deploy makes them in production. The postgres
-- image runs this on a new volume (docker-compose.yml), and scripts/dev.sh
-- runs it again on every start, for a volume made before it; it changes
-- nothing the second time. Local development only.
SELECT 'CREATE ROLE lobby LOGIN PASSWORD ''lobby'''
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'lobby')
\gexec

SELECT 'CREATE DATABASE lobby OWNER lobby'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'lobby')
\gexec
