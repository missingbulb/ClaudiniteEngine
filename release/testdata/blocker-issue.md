@claudinite/cli-rc 1.60928.3 fails the post-publish smoke on darwin-arm64

`@claudinite/cli-rc 1.60928.3` is published, and the darwin-arm64 leg of `smoke-published` failed against registry.npmjs.org: a member pinned to it on darwin-arm64 cannot run its engine.

While this issue is open with the `release-blocker` label, `promote.yml` refuses to promote 1.60928.3. Close it once the failure is understood; the fix ships as a new version, since nothing is republished.

Run: https://github.com/missingbulb/ClaudiniteEngine/actions/runs/1

The last 60 lines of the leg's log:

~~~~
line 43
line 44
line 45
line 46
line 47
line 48
line 49
line 50
line 51
line 52
line 53
line 54
line 55
line 56
line 57
line 58
line 59
line 60
line 61
line 62
line 63
line 64
line 65
line 66
line 67
line 68
line 69
line 70
line 71
line 72
line 73
line 74
line 75
line 76
line 77
line 78
line 79
line 80
line 81
line 82
line 83
line 84
line 85
line 86
line 87
line 88
line 89
line 90
line 91
line 92
line 93
line 94
line 95
line 96
line 97
line 98
line 99
line 100
~~~ a fence in the log
smoke-platform: FAIL: selftest version: version 0.0.0
~~~~
