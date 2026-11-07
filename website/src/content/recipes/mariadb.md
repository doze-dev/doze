---
title: MariaDB
description: Run MariaDB, the MySQL-compatible database, with its users and grants declared in doze.hcl.
---

Run a MySQL-compatible database for your app, with its users and grants
declared beside it.

MariaDB is the community fork of MySQL, begun by MySQL's original authors and
licensed GPLv2. It speaks the MySQL protocol, so `mysql://` drivers, the
`mysql` and `mariadb` clients, and GUIs connect unchanged.

## A database

```hcl
module mariadb "db" {
  version = "11.4"
}
```

The database is named after the block and created on the first start.

```sh
doze url db
# mysql://root@db.shop.doze:3306/db
```

Hand it to an app by reference: `DATABASE_URL = module.db.url`.

## Users and grants

```hcl
module mariadb "app" {
  version       = "11.4"
  character_set = "utf8mb4"

  user "app" {
    password = "secret"
    host     = "%"
  }

  grant {
    user       = "app"
    privileges = ["ALL PRIVILEGES"]
    database   = "app"
  }
}
```

An app that logs in as `app` builds its URL around the address:

```hcl
app "api" {
  run  = "php artisan serve --port=$PORT"
  port = 8000
  env  = { DATABASE_URL = "mysql://app:secret@${module.app.address}/app" }
}
```

## Server settings

`settings` adds lines to the `[mysqld]` section of the server's config:

```hcl
module mariadb "db" {
  version  = "11.4"
  settings = { max_connections = "50" }
}
```
