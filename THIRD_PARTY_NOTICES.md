# Third-party notices

Books depends directly on the following open-source packages. Their complete
license texts and copyright notices are available from the linked upstream
source distributions and must be retained when applicable to redistribution.

| Package | Version | License |
| --- | --- | --- |
| [`github.com/mattn/go-sqlite3`](https://github.com/mattn/go-sqlite3/tree/v1.14.32) | 1.14.32 | MIT |
| [`github.com/pelletier/go-toml/v2`](https://github.com/pelletier/go-toml/tree/v2.4.3) | 2.4.3 | MIT |
| [`github.com/spf13/cobra`](https://github.com/spf13/cobra/tree/v1.10.2) | 1.10.2 | Apache-2.0 |
| [`github.com/spf13/pflag`](https://github.com/spf13/pflag/tree/v1.0.10) | 1.0.10 | BSD-3-Clause |
| [`github.com/inconshreveable/mousetrap`](https://github.com/inconshreveable/mousetrap/tree/v1.1.0) | 1.1.0 | Apache-2.0 |

This inventory describes the current source tree. Review and update it whenever
dependencies change and before distributing a later source or binary release.

## MCP runtime dependencies

The official MCP Go SDK is pinned to v1.7.0. Its upstream license is undergoing
an Apache-2.0/MIT transition; the complete license and original notices are retained
below. Books uses SDK code; no upstream documentation text is incorporated.

| Package | Version | License notice |
| --- | --- | --- |
| `github.com/google/jsonschema-go` | v0.4.3 | [MIT](licenses/github.com_google_jsonschema-go-LICENSE.txt) |
| `github.com/modelcontextprotocol/go-sdk` | v1.7.0 | [Apache-2.0 and MIT (transition; upstream documentation CC-BY-4.0)](licenses/github.com_modelcontextprotocol_go-sdk-LICENSE.txt) |
| `github.com/segmentio/asm` | v1.1.3 | [MIT](licenses/github.com_segmentio_asm-LICENSE.txt) |
| `github.com/segmentio/encoding` | v0.5.4 | [MIT](licenses/github.com_segmentio_encoding-LICENSE.txt) |
| `github.com/yosida95/uritemplate/v3` | v3.0.2 | [BSD-3-Clause](licenses/github.com_yosida95_uritemplate_v3-LICENSE.txt) |
| `golang.org/x/oauth2` | v0.35.0 | [BSD-3-Clause](licenses/golang.org_x_oauth2-LICENSE.txt) |
| `golang.org/x/sync` | v0.20.0 | [BSD-3-Clause](licenses/golang.org_x_sync-LICENSE.txt) |
| `golang.org/x/sys` | v0.44.0 | [BSD-3-Clause](licenses/golang.org_x_sys-LICENSE.txt) |
| `golang.org/x/time` | v0.15.0 | [BSD-3-Clause](licenses/golang.org_x_time-LICENSE.txt) |

## Web interface

The optional React/shadcn interface has its dependency versions pinned in
[web/package-lock.json](web/package-lock.json). Full installed upstream license
texts and notices, including font and icon notices, are retained in
[web/public/THIRD_PARTY_NOTICES.txt](web/public/THIRD_PARTY_NOTICES.txt) and copied
into built static assets. See [web maintenance](web/README.md#components-and-maintenance)
for necessity, security and update procedures.
