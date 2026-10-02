# OpenStack Database Exporter

This is a Prometheus exporter that aims to pull some of the data that is
provided by the [OpenStack Exporter](https://github.com/openstack-exporter/openstack-exporter)
but directly from the database.

## Why?

The existance of this exporter is an OpenStack bug.  At scale, the APIs
become extremely sluggish at operating with large number of API requests.

In addition, certain queries can be done far more effectively directly
without going through many different API calls.  The goal of this exporter
is to be operator-facing and providing high performance for large scale
clouds.

## Database queries

Service database APIs, SQL definitions, and integration schema fixtures are
maintained in [openstackdb](https://github.com/vexxhost/openstackdb). This exporter
owns Prometheus collectors and metric-output tests. Query changes belong in
openstackdb; update the pinned module version here after validating compatibility.

```sh
go test -short -race ./...
# Requires Docker for MariaDB fixtures and exporter end-to-end tests.
go test -race -tags integration -timeout 15m ./...
go build ./cmd/openstack-database-exporter
```

Integration tests load embedded fixtures from the pinned openstackdb module.
Database URLs and exporter configuration remain the same.

## Load balancer (Octavia) quotas

Per-project Soft-draft metrics Soft-matching the API exporter (no `reserved`):

- `openstack_loadbalancer_quota_{loadbalancer,listener,pool,member,healthmonitor,l7policy,l7rule}{type,tenant,tenant_id}`

`limit` Soft-from the Octavia `quotas` table (defaults `-1`). `in_use` Soft-for loadbalancer/listener/pool Soft-from object counts by `project_id`. Soft-requires Soft-fork openstackdb Soft-pin Soft-(see Soft-draft Soft-PR Soft-body Soft-; Soft-upstream Soft-pin Soft-later Soft-).
