#!/usr/bin/env sh

# Builds the ssmachmos program and installs it so it can be run from the terminal.
# Run it from the project's main folder, because the build path below is relative.

# BUG: there is no "set -e", so if the build fails the script goes on and mv moves an old or missing file.
# Builds only this one file. The program is written as "ssmachmos" in the current folder.
# BUG: other files in the same folder are not built.
go build ./cmd/ssmachmos/ssmachmos.go
# Moves the program to INSTALL_PATH, or to /usr/local/bin if INSTALL_PATH is not set.
# /usr/local/bin usually needs admin rights, so this can fail without sudo.
# BUG: the path is not in quotes, so a path with spaces breaks.
mv ./ssmachmos ${INSTALL_PATH:-"/usr/local/bin"}
