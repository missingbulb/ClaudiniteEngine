---
name: hello-guide
description: The hello pack's forced-loading probe. Load it before editing under HELLO_SCOPED/, running a hello-call command, acting on a HELLO PROMPT or on a HELLO_RESULT.
metadata:
  force-load-on-file-edits-paths:
    - 'HELLO_SCOPED/**'
  force-load-on-tool-calls:
    - 'Bash.command /\bhello-call\b/'
  force-load-on-prompts-matching:
    - '/\bHELLO PROMPT\b/'
  force-load-on-tool-results-matching:
    - 'Bash /HELLO_RESULT/'
---

This skill exists to be forced: once it is loaded, the edit, the call, the prompt or the result
that asked for it goes ahead. There is nothing else to do.
