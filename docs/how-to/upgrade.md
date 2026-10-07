---
title: Upgrade Seagull and regenerate
description: Select a release deliberately and read its compatibility notes. Record
  the old sgl version output and ensure the working tree contains no unreviewed generated
  changes.
section: How-to guides
subsection: Tooling
order: 960
---

# Upgrade Seagull and regenerate

Choose a release, update the pinned tool and review what regeneration changes. Start with the existing generated output committed so the diff belongs to this upgrade.

## Update the tool version

Read the notes of the [release](https://github.com/piko-sh/seagull/releases) you are moving to, then replace `vX.Y.Z` below with the published tag you selected:

```sh
go tool sgl version
seagull_release='vX.Y.Z'
go get -tool "seagull.sh/seagull/cmd/sgl@${seagull_release}"
go tool sgl version
```

If the application also imports the generator, `seagull.sh/seagull`, or a package of the `sdk` kit, such as the migration runtime in `seagull.sh/seagull/sdk/migrate`, update its `seagull.sh/seagull` module dependency to that release too. Then run `go build ./...`, which reports each import or name the release changed.

## Check and regenerate

```sh
go tool sgl config validate
go tool sgl vet --strict
go tool sgl generate --diff
```

Resolve new diagnostics and review the proposed API changes before writing. A query with an error can be left out of generated output, so continuing with that error could remove a method.

```sh
go tool sgl generate
go test ./...
go tool sgl generate --check
```

Run the application's database integration tests as well. Commit the dependency changes, SQL adjustments and reviewed generated output together.

## Check migration-runtime compatibility

If the application embeds migrations, review persisted history names and locking changes before a rolling deployment. Old and new processes must coordinate on the same history and lock. The [compatibility reference](../reference/compatibility.md) describes these contracts.
