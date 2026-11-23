# Custom prompts

All prompts live in `internal/prompts/prompts.go` and are baked into the binary.
Override any of them in `podcasts.yaml`:

```yaml
prompts:
  overrides:
    script.system: "You are a podcast writer. Your voice is witty and concise..."
  files:
    critic.system: "path/to/custom-critic.txt"
```

Priority: inline (`overrides`) > file (`files`) > baked-in default.
The file's path may be absolute or relative to the config file's directory.
Validation: unknown prompt names and invalid templates fail at startup.
Known prompt names:

- research.system, research.user
- summarize.system, summarize.user
- script.system, script.user
- critic.system, critic.user

Template variables are Go `text/template` data maps; see `internal/prompts/prompts.go`
for the exact keys each prompt expects.
