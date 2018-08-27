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
	pkgCount := 0

	for _, testPackage := range report.Packages {
		pkgCount++

		if len(testPackage.Tests) == 0 {
			writer.WriteByte('\n')
			break
		} else {
			writer.WriteByte(',')
		}

		bytes, err := json.Marshal(testPackage.Tests)

		if err != nil {
			return err
		}

		writer.Write(bytes)
	}

	writer.Flush()

	return nil
}
