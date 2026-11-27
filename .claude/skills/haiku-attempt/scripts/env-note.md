Environment note (tooling only):
- The `go` and `gofmt` found on PATH in this machine are shims that hang. Always invoke the toolchain by absolute path: `/opt/homebrew/bin/go` and `/opt/homebrew/bin/gofmt`.
- These Bash commands are permitted, run from the current directory, one simple command per call (no `;`, `&&`, pipes, heredocs, or `$VAR` expansions):
  `/opt/homebrew/bin/go test ./...`, `/opt/homebrew/bin/go build ./...`, `/opt/homebrew/bin/go vet ./...`, `/opt/homebrew/bin/gofmt -l .`, `git status`, `git diff`, `git add <files>`, `git commit -m "<message>"`, `git log`, `ls`, `pwd`, `find . ...`, `grep ...`.
- Bash runs in a sandbox limited to this directory; harmless warnings such as "go telemetry ... operation not permitted" can be ignored.
- Other tools or commands are denied automatically. A denial of one command does not mean Bash is unavailable: keep using the permitted commands above.
- Edit files only with the Edit/Write tools.
- You are expected to run the tests and make the commit yourself before finishing.
