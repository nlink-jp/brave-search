# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Scaffold: config resolution (sectioned TOML under `[api]` / `[search]` /
  `[context]` / `[answers]`, `BRAVE_SEARCH_*` environment variables, strict
  keys, local range validation), the CLI shell with the version and help
  contracts, and the stdio MCP server skeleton exposing `get_usage`.
