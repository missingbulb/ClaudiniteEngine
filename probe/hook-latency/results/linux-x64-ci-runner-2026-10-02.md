# Hook latency (38 packs): linux-x64, 2026-10-02

Produced by `sh probe/hook-latency/run.sh --node <Claudinite checkout> --runs 50` in CI's parity job (ubuntu-24.04) at d27f658, copied from the job log. Times are wall-clock milliseconds.

| Item | Runs | Median | p95 | Max |
| --- | ---: | ---: | ---: | ---: |
| derivation from the tree (in process) | 50 | 13.2 ms | 18.9 ms | 25.5 ms |
| pre-tool-use Read, no declaration (no transcript) | 50 | 16.1 ms | 16.6 ms | 16.8 ms |
| pre-tool-use Bash plain (no transcript) | 50 | 16.0 ms | 16.6 ms | 16.7 ms |
| pre-tool-use Bash held, git commit (no transcript) | 50 | 7.7 ms | 8.4 ms | 8.8 ms |
| pre-tool-use Edit under a scoped path (no transcript) | 50 | 7.9 ms | 9.5 ms | 11.0 ms |
| pre-tool-use mcp tool (no transcript) | 50 | 16.0 ms | 16.8 ms | 17.2 ms |
| post-tool-use Bash result (no transcript) | 50 | 8.0 ms | 8.7 ms | 8.8 ms |
| user-prompt-submit prompt (no transcript) | 50 | 7.9 ms | 8.5 ms | 8.6 ms |
| pre-tool-use Read, no declaration (5 MB transcript) | 50 | 16.1 ms | 16.8 ms | 17.2 ms |
| pre-tool-use Bash plain (5 MB transcript) | 50 | 16.1 ms | 17.5 ms | 19.2 ms |
| pre-tool-use Bash held, git commit (5 MB transcript) | 50 | 74.9 ms | 76.0 ms | 76.2 ms |
| pre-tool-use Edit under a scoped path (5 MB transcript) | 50 | 74.9 ms | 76.1 ms | 76.3 ms |
| pre-tool-use mcp tool (5 MB transcript) | 50 | 16.3 ms | 17.0 ms | 17.3 ms |
| post-tool-use Bash result (5 MB transcript) | 50 | 8.0 ms | 8.5 ms | 8.6 ms |
| user-prompt-submit prompt (5 MB transcript) | 50 | 7.8 ms | 8.5 ms | 8.8 ms |

Host:

- `uname -a`: Linux runnervm8df0l 6.17.0-1022-azure #22-Ubuntu SMP Mon Jul 27 17:24:03 UTC 2026 x86_64 x86_64 x86_64 GNU/Linux
- `go version`: go version go1.24.13 linux/amd64
- `node --version`: v22.23.3
- CPU: INTEL(R) XEON(R) PLATINUM 8573C
