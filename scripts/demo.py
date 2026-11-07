#!/usr/bin/env python3
"""Local-only demo lifecycle. No DATABASE_URL, shell interpolation, or remote DB support."""
import argparse
import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
DB = 'openlocal_demo'
MARKER = 'openlocal-demo-v1-local-only'
APP = 'app_3F6Bk3hXx7qbn8arLYrVovyU02K'


def run(args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('action', choices=['setup', 'seed', 'check', 'reset', 'verify-api'])
    p.add_argument('--container', default='openlocal-api_postgres_1')
    p.add_argument('--owner-clerk-id', help='Real Clerk DEVELOPMENT user; verified with Clerk CLI')
    p.add_argument('--confirm-reset', action='store_true')
    a = p.parse_args()
    # Refuse remote engines, arbitrary containers and production URLs altogether.
    info = json.loads(run(['podman', 'inspect', a.container]))[0]
    labels = info['Config'].get('Labels', {})
    if labels.get('com.docker.compose.service') != 'postgres' or labels.get('com.docker.compose.project') != 'openlocal-api':
        p.error('Target must be the local openlocal-api compose postgres service')
    if not any(m['Destination'] == '/var/lib/postgresql/data' and m['Type'] == 'volume' for m in info['Mounts']):
        p.error('Expected local PostgreSQL volume missing')
    connection = json.loads(run(['podman', 'info', '--format', 'json']))
    if connection.get('host', {}).get('serviceIsRemote', False):
        p.error('Remote Podman is forbidden')

    def sql(body, db=DB, variables=None):
        cmd = ['podman', 'exec', '-i', a.container, 'sh', '-c',
               'exec psql -X -qAt -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" "$@"', 'demo-psql', '-d', db]
        for k, v in (variables or {}).items():
            cmd += ['-v', k + '=' + v]
        return run(cmd, input=body).strip()

    exists = sql("SELECT 1 FROM pg_database WHERE datname='openlocal_demo'", 'postgres') == '1'
    if a.action == 'setup' and not exists:
        sql('CREATE DATABASE openlocal_demo;', 'postgres')
        sql("COMMENT ON DATABASE openlocal_demo IS 'openlocal-demo-v1-local-only';", 'postgres')
    elif not exists:
        p.error('Run setup first')
    if sql("SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()") != MARKER:
        p.error('Refusing an unmarked database; existing databases are never adopted')
    print('Verified local compose PostgreSQL / openlocal_demo (demo marker present).')
    # Serialize runners for migration and seed. Each SQL payload is transactional.
    if a.action == 'setup':
        sql('CREATE TABLE IF NOT EXISTS demo_migrations(version bigint PRIMARY KEY);')
        for f in sorted((ROOT / 'db/migrations').glob('*.up.sql')):
            version = int(f.name.split('_')[0])
            sql("BEGIN; SELECT pg_advisory_xact_lock(71403811);\n" +
                f"SELECT NOT EXISTS(SELECT 1 FROM demo_migrations WHERE version={version}) AS apply \\gset\n\\if :apply\n" +
                f.read_text() + f'\nINSERT INTO demo_migrations VALUES({version});\n\\endif\nCOMMIT;')
        print('Demo schema ready. Existing API/database targets were not changed.')
        return
    if a.action == 'verify-api':
        print(run(['podman', 'exec', '-w', '/app', 'openlocal-api_api_1', 'go', 'run', './cmd/demo-check']))
        return
    if a.action == 'check':
        print(sql((ROOT / 'db/seeds/demo_check.sql').read_text()))
        return
    if a.action == 'reset' and not a.confirm_reset:
        p.error('reset requires --confirm-reset (deletes only the three demo business aggregates)')
    if not a.owner_clerk_id or not re.fullmatch(r'user_[A-Za-z0-9]+', a.owner_clerk_id):
        p.error('--owner-clerk-id must identify a real development Clerk user')

    def clerk(path, body=None):
        cmd = ['clerk', 'api', path, '--app', APP, '--instance', 'dev']
        if body:
            cmd += ['--data', json.dumps(body)]
            run(['fish', '-lc', ' '.join(__import__('shlex').quote(s) for s in cmd + ['--dry-run'])])
            cmd += ['--yes']
        return json.loads(run(['fish', '-lc', ' '.join(__import__('shlex').quote(s) for s in cmd)]))

    clerk('/users/' + a.owner_clerk_id)
    variables = {'owner': a.owner_clerk_id, 'reset': 'true' if a.action == 'reset' else 'false'}
    for key, name in [('tienda', 'Abarrotes La Esquina'), ('pan', 'Panadería Amanecer'), ('salon', 'Salón Jacaranda')]:
        slug = 'ol-demo-' + key
        found = clerk('/organizations?slug=' + slug).get('data', [])
        org = next((o for o in found if o['slug'] == slug), None)
        if org is None:
            org = clerk('/organizations', {'name': name + ' · Demo', 'slug': slug, 'created_by': a.owner_clerk_id})
        members = clerk('/organizations/' + org['id'] + '/memberships?limit=100')['data']
        if not any(m['public_user_data']['user_id'] == a.owner_clerk_id and m['role'] == 'org:admin' for m in members):
            p.error('Demo owner must already be admin of ' + slug)
        variables[key + '_org'] = org['id']
    print(sql((ROOT / 'db/seeds/demo.sql').read_text(), variables=variables))
    print(sql((ROOT / 'db/seeds/demo_check.sql').read_text()))


if __name__ == '__main__':
    try:
        main()
    except subprocess.CalledProcessError as e:
        # Never echo command/env; postgres diagnostics have no connection string.
        print('Demo operation failed: ' + (e.stderr or 'command failed')[-1800:], file=sys.stderr)
        sys.exit(1)
