package formatter

import (
	"bufio"
	"encoding/json"
	"io"

	"github.com/jeffreyCline/go-junit-report/parser"
)

// JSONReport writes a JUnit xml representation of the given report to w
func JSONReport(report *parser.Report, w io.Writer) error {

	writer := bufio.NewWriter(w)

	for _, testPackage := range report.Packages {
		bytes, err := json.Marshal(testPackage.Tests)

		if err != nil {
			return err
		}

		writer.Write(bytes)
		writer.WriteByte('\n')
	}

	writer.Flush()

	return nil
}
