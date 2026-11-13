// SPDX-License-Identifier: MPL-2.0

// Package legal provides the licenses and source information shipped with Lefalock.
package legal

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
)

// SourceURL is the source archive for this version, injected by release builds.
var SourceURL string

//go:embed MPL-2.0.txt
var license string

//go:embed THIRD_PARTY_NOTICES.txt
var notices string

const scope = "Lefalock's own Go source files in cmd/ and internal/ (including repository tests), and its user guide, are licensed under MPL 2.0. Third-party components and license texts retain their original licenses."

// RunCommand prints legal information without accessing configuration or the vault.
func RunCommand(args []string, version string, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] != "licenses" {
		return false, nil
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h" || args[1] == "help") {
		_, err := io.Copy(out, strings.NewReader("Usage: lefalock licenses\nPrint licenses, third-party notices, and corresponding source information.\n"))
		return true, err
	}
	if len(args) != 1 {
		return true, errors.New("usage: lefalock licenses")
	}
	source := SourceURL
	if source == "" {
		source = "Not specified for this build. Request the matching source from your distributor."
	}
	text := fmt.Sprintf("Lefalock %s\n\n%s\n\nCorresponding source: %s\nThe source of Lefalock's MPL-covered code is available under MPL 2.0.\nIf you cannot access the source archive, request it from your distributor.\n\n=== Mozilla Public License 2.0 ===\n\n%s\n=== Third-party licenses and notices ===\n\n%s", version, scope, source, license, notices)
	_, err := io.Copy(out, strings.NewReader(text))
	return true, err
}
