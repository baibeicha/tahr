# Interactive Regex Sandbox (`regex-tester`)

Real-time regular expression development and verification playground in Tahr IDE.

## Features
- **RE2 Engine**: Evaluates patterns using Go's linear-time RE2 regular expression engine (guaranteed ReDoS-safe with zero catastrophic backtracking).
- **Live Match Highlighting**: Matches, submatches, and named capture groups (`(?P<name>...)`) are highlighted instantly as you type.
- **Substitution Preview**: Test regex replacement strings (`${1}`, `$name`) with real-time transformed text output.
- **Keybinding**: Press `Ctrl+Shift+R` to open the regex sandbox modal.
