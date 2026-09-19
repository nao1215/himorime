package cli

import (
	"fmt"
	"io"

	"github.com/nao1215/himorime/internal/diag"
)

func diagnosticf(w io.Writer, code diag.Code, format string, args ...any) {
	fmt.Fprintf(w, "%s: %s\n", code.String(), fmt.Sprintf(format, args...))
}
