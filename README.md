# modelsrv-ldap-sensor

An EmELand sensor that reads identities and groups from an LDAP directory and publishes them as
phase 2 (IAM) resources: every selected person entry becomes an `Identity`, every selected group
entry becomes a `Group` whose `members` list the identities of the group.

The sensor runs its own modelsrv endpoint and forwards all changes to the configured subscribers
(usually a central modelsrv). It scans the directory at a fixed interval and only sends what changed
since the last scan: new entries are created, changed entries updated and vanished entries deleted.
If a search fails, nothing is deleted; the next scan tries again.

## Running

```sh
make run                                   # uses config/sensor.yaml
go run ./cmd/modelsrv-ldap-sensor --config my.yaml --listen 0.0.0.0:24300 \
    --self-url http://ldap-sensor.my-ns.svc:24300/api/
```

| Flag | Environment variable | Default | Meaning |
|---|---|---|---|
| `--config` | `SENSOR_CONFIG` | `config/sensor.yaml` | Path to the YAML config |
| `--listen` | `SENSOR_LISTEN_ADDR` | `localhost:24300` | Address of the sensor's own modelsrv API |
| `--self-url` | `SENSOR_SELF_URL` | `http://<listen>/api/` | URL under which subscribers reach the sensor. It is stamped as the origin of every event, so set it when the sensor runs behind a Service or binds to `0.0.0.0` |
| `--poll-interval` | `SENSOR_POLL_INTERVAL` | from config | Overrides `pollInterval` |

The bind password can be passed as `LDAP_BIND_PASSWORD`; it takes precedence over the config file.
Keep real passwords out of the YAML file.

## Configuration

See [config/sensor.yaml](config/sensor.yaml) for an example.

| Key | Default | Meaning |
|---|---|---|
| `subscribers` | none | Base API URLs of the model servers that receive the events |
| `pollInterval` | `5m` | Scan interval |
| `ldap.url` | required | `ldap://` or `ldaps://` URL of the server |
| `ldap.bindDN`, `ldap.bindPassword` | anonymous bind | Credentials for the search. A bind DN without password is rejected |
| `ldap.startTLS` | `false` | Upgrade an `ldap://` connection with StartTLS (not with `ldaps://`) |
| `ldap.caFile` | system CAs | Extra trusted CA certificates (PEM) |
| `ldap.insecureSkipVerify` | `false` | Skip certificate checks. Only for testing |
| `ldap.pageSize` | `500` | Page size of the paged search |
| `identities.baseDN` | required | Sub-tree that holds the identities |
| `identities.filter` | `(objectClass=inetOrgPerson)` | Search filter for identities |
| `identities.scope` | `sub` | `sub`, `one` or `base` |
| `identities.nameAttribute` | `cn` | Attribute used as display name |
| `identities.descriptionAttribute` | none | Attribute used as description |
| `identities.idAttribute` | none (DN) | Attribute with a stable id, see below |
| `groups.*` | as for identities, filter `(objectClass=groupOfNames)` | Same keys for the group entries |
| `groups.memberAttribute` | `member` | Attribute that lists the DNs of the members |

The defaults fit an OpenLDAP style tree. Other layouts, for example Active Directory, work through
the filter and attribute settings (e.g. `(objectClass=user)`, `sAMAccountName`, `objectGUID`).

### Identifiers

EmELand resources need ids that do not change. If `idAttribute` is set and holds a UUID (for example
`entryUUID` on OpenLDAP), that UUID is used, so an entry keeps its id when it is moved in the tree.
Otherwise the id is a UUID v5 derived from the server host, the resource kind and the entry's DN (or
the non-UUID value of `idAttribute`).

### Annotations

| Key | On | Value |
|---|---|---|
| `emeland.io/p2-identity-ldap-distinguished-name` | Identity | DN of the entry |
| `emeland.io/p2-identity-ldap-url` | Identity | `ldap.url` |
| `emeland.io/p2-group-ldap-distinguished-name` | Group | DN of the entry |
| `emeland.io/p2-group-ldap-url` | Group | `ldap.url` |

## Limitations

- Members are matched to identities by DN. A member that is not one of the selected identities is
  skipped (and logged at debug level). Members given as plain user names (`posixGroup` with
  `memberUid`) are not supported yet.
- DNs are compared after a simple normalisation (case and spaces around separators). Escaped
  characters in DNs are not handled.
- Nested groups are not resolved, and Active Directory's ranged attribute retrieval for very large
  groups is not implemented.
- A webhook to trigger a re-scan is tracked in #2.

## CI and releases

GitLab CI (`.gitlab-ci.yml`) uses the shared baseline from modelsrv: lint, build, unit tests, image
build and scans.

GitLab push-mirrors `main` and tags to GitHub. There, `.github/workflows/ci.yml` runs all tests
including the OpenLDAP integration test, and `container-image-publish.yml` pushes
`ghcr.io/emeland-io/modelsrv-ldap-sensor` for `main` and every `v*` tag. To release, push a `vX.Y.Z`
tag to GitLab.



## Development

```sh
make ci      # mod download/verify, lint, build, test
make test
```

The integration test in `test/integration` starts an OpenLDAP container (`osixia/openldap`) with
testcontainers and loads `test/testdata/people-groups.ldif`. It needs Docker or Podman and is skipped
with `go test -short` or when no container runtime is available.

## License

Apache 2.0, see [LICENSE](LICENSE).
