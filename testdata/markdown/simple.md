# Getting Started

Welcome to the getting started guide. This document will walk you through
the basics of using this library.

## Installation

To install, run the following command:

```bash
go get github.com/codanael/docserve
```

After installing, you can import the package in your Go code.

## Configuration

Configuration is done via a YAML file or environment variables.

### Database

Configure the database connection using the following settings:

```yaml
database:
  host: localhost
  port: 5432
  name: mydb
```

The database section supports all standard PostgreSQL connection options.

### Cache

Configure the cache layer for improved performance:

The cache uses an in-memory LRU by default, but can be configured to use
Redis for distributed caching across multiple instances.
