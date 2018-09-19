package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/jeffreyCline/go-junit-report/formatter"
	"github.com/jeffreyCline/go-junit-report/parser"
)

var (
	noXMLHeader   bool
	packageName   string
	goVersionFlag string
	setExitCode   bool
	formatJSON    bool
)

func init() {
	flag.BoolVar(&noXMLHeader, "no-xml-header", false, "do not print xml header")
	flag.StringVar(&packageName, "package-name", "", "specify a package name (compiled test have no package name in output)")
	flag.StringVar(&goVersionFlag, "go-version", "", "specify the value to use for the go.version property in the generated XML")
	flag.BoolVar(&setExitCode, "set-exit-code", false, "set exit code to 1 if tests failed")
	flag.BoolVar(&formatJSON, "format-json", false, "save detailed run data as a Json file")
}

func main() {
	flag.Parse()

	// Read input
	report, err := parser.Parse(os.Stdin, packageName)
	if err != nil {
		fmt.Printf("Error reading input: %s\n", err)
		os.Exit(1)
	}

	// Write xml
	if !formatJSON {
		err = formatter.JUnitReportXML(report, noXMLHeader, goVersionFlag, os.Stdout)
		if err != nil {
			fmt.Printf("Error writing XML: %s\n", err)
			os.Exit(1)
		}
	}

	// Write json
	if formatJSON {
		err = formatter.JSONReport(report, os.Stdout)
		if err != nil {
			fmt.Printf("Error writing JSON: %s\n", err)
			os.Exit(1)
		}
	}

	if setExitCode && report.Failures() > 0 {
		os.Exit(1)
	}
}
