package parser

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Result represents a test result.
type Result int

// Test result constants
const (
	PASS Result = iota
	FAIL
	SKIP
)

// ACTION Represents what action is being performed during the test step
type ACTION int

// console control the flow of debug messages
type console bool

// What the test step is doing
const (
	UNKNOWN ACTION = 0
	CREATE  ACTION = 1
	UPDATE  ACTION = 2
	DESTROY ACTION = 3
)

// ResourceTime Contains the result if individual actions per resource
type ResourceTime struct {
	ResourceName string
	Duration     float64
	Action       []ACTION
}

// Report is a collection of package tests.
type Report struct {
	Packages []Package
}

// Package contains the test results of a single package.
type Package struct {
	Name        string
	Time        float64
	Tests       []*Test
	CoveragePct string
}

// Test contains the results of a single test.
type Test struct {
	Name              string
	Time              float64
	TestOverhead      float64
	CreateTime        float64
	CreateDestroyTime float64
	Result            Result
	Output            []string
	CreateText        []string
	DestroyText       []string
	Steps             []ResourceTime
	CleanUp           []ResourceTime
}

var (
	regexStatus        = regexp.MustCompile(`^\s*--- (PASS|FAIL|SKIP): (.+) \((\d+\.\d+)(?: seconds|s)\)$`)
	regexCoverage      = regexp.MustCompile(`^coverage:\s+(\d+\.\d+)%\s+of\s+statements(?:\sin\s.+)?$`)
	regexResult        = regexp.MustCompile(`^(ok|FAIL)\s+([^ ]+)\s+(?:(\d+\.\d+)s|(\[\w+ failed]))(?:\s+coverage:\s+(\d+\.\d+)%\sof\sstatements(?:\sin\s.+)?)?$`)
	regexOutput        = regexp.MustCompile(`(    )*\t(.*)`)
	regexSummary       = regexp.MustCompile(`^(PASS|FAIL|SKIP)$`)
	regexTimeFormat    = regexp.MustCompile(`(\d{4})/(\d{2})/(\d{2})\s(\d{2}):(\d{2}):(\d{2})`)
	regexCreationStart = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2}\s\d{2}:\d{2}:\d{2})\s\[INFO\]\sTest:\sUsing\s([\w-]+)\sas\stest\sregion$`)

	regexDestroyStart            = regexp.MustCompile(`^\d{4}/\d{2}/\d{2}.\d{2}:\d{2}:\d{2}.\SWARN\S.Test:.Executing.destroy.step`)
	regexIsDiff                  = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2}).(\d{2}:\d{2}:\d{2}).(\SWARN\S).(Test: Step plan: DIFF:)`)
	regexIsCreateUpdateOrDestroy = regexp.MustCompile(`^(CREATE|UPDATE|DESTROY):.(.*)`)
	regexIsGraphTypeApply        = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2}).(\d{2}):(\d{2}):(\d{2}).(\SINFO\S).(terraform:.building.graph:.GraphTypeApply)`)
	regexIsGraphTypePlan         = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2}).(\d{2}):(\d{2}):(\d{2}).(\SINFO\S).(terraform:.building.graph:.GraphTypePlan)`)
	regexIsGraphTypeDestroy      = regexp.MustCompile(`^(\d{4})/(\d{2})/(\d{2}).(\d{2}):(\d{2}):(\d{2}).(\SINFO\S).(terraform:.building.graph:.GraphTypePlanDestroy)`)
)

// Console write debug output
var Console console = true

// Parse parses go test output from reader r and returns a report with the
// results. An optional pkgName can be given, which is used in case a package
// result line is missing.
func Parse(r io.Reader, pkgName string) (*Report, error) {
	reader := bufio.NewReader(r)

	report := &Report{make([]Package, 0)}

	// keep track of tests we find
	var tests []*Test

	// sum of tests' time, use this if current test has no result line (when it is compiled test)
	testsTime := 0.0

	// current test
	var cur string

	// keep track if we've already seen a summary for the current test
	var seenSummary bool

	// coverage percentage report for current package
	var coveragePct string

	// stores mapping between package name and output of build failures
	var packageCaptures = map[string][]string{}

	// the name of the package which it's build failure output is being captured
	var capturedPackage string

	// capture any non-test output
	var buffer []string

	// used to track which section of the test case is in
	var isCreate = false

	// used to simulate a continue
	var skipCheck bool

	// parse lines
	for {
		l, _, err := reader.ReadLine()
		if err != nil && err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		skipCheck = false
		line := string(l)

		if strings.HasPrefix(line, "=== RUN ") {
			// new test
			cur = strings.TrimSpace(line[8:])
			tests = append(tests, &Test{
				Name:              cur,
				Result:            FAIL,
				Time:              0.0,
				TestOverhead:      0.0,
				CreateTime:        0.0,
				CreateDestroyTime: 0.0,
				Output:            make([]string, 0),
				CreateText:        make([]string, 0),
				DestroyText:       make([]string, 0),
				Steps:             make([]ResourceTime, 0),
				CleanUp:           make([]ResourceTime, 0),
			})

			// clear the current build package, so output lines won't be added to that build
			capturedPackage = ""
			isCreate = true
			seenSummary = false
		} else if matches := regexResult.FindStringSubmatch(line); len(matches) == 6 {
			if matches[5] != "" {
				coveragePct = matches[5]
			}
			if strings.HasSuffix(matches[4], "failed]") {
				// the build of the package failed, inject a dummy test into the package
				// which indicate about the failure and contain the failure description.
				tests = append(tests, &Test{
					Name:   matches[4],
					Result: FAIL,
					Output: packageCaptures[matches[2]],
				})
			} else if matches[1] == "FAIL" && len(tests) == 0 && len(buffer) > 0 {
				// This package didn't have any tests, but it failed with some
				// output. Create a dummy test with the output.
				tests = append(tests, &Test{
					Name:   "Failure",
					Result: FAIL,
					Output: append(make([]string, len(buffer), cap(buffer)), buffer...),
				})
			}

			// all tests in this package are finished
			report.Packages = append(report.Packages, Package{
				Name:        matches[2],
				Time:        parseTime(matches[3]),
				Tests:       tests,
				CoveragePct: coveragePct,
			})

			buffer = buffer[0:0]
			tests = make([]*Test, 0)
			coveragePct = ""
			cur = ""
			testsTime = 0
		} else if matches := regexStatus.FindStringSubmatch(line); len(matches) == 4 {
			cur = matches[2]
			test := findTest(tests, cur)
			if test != nil {
				skipCheck = true

				// test status
				if matches[1] == "PASS" {
					test.Result = PASS
				} else if matches[1] == "SKIP" {
					test.Result = SKIP
					clearCreateDestroyText(test)
				} else {
					test.Result = FAIL
					clearCreateDestroyText(test)
				}

				test.Output = append(test.Output, buffer...)
				buffer = buffer[0:0]

				test.Name = matches[2]

				// in ms.
				testTime := parseTime(matches[3])
				test.Time = testTime

				if test.Result == PASS {
					test = processCreateDestroySections(test)
				}

				testsTime += testTime
			}
		} else if matches := regexCoverage.FindStringSubmatch(line); len(matches) == 2 && !skipCheck {
			coveragePct = matches[1]
		} else if matches := regexOutput.FindStringSubmatch(line); capturedPackage == "" && len(matches) == 3 && !skipCheck {
			// Sub-tests start with one or more series of 4-space indents, followed by a hard tab,
			// followed by the test output
			// Top-level tests start with a hard tab.
			test := findTest(tests, cur)
			if test != nil {
				skipCheck = true
				test.Output = append(test.Output, matches[2])
			}
		} else if strings.HasPrefix(line, "# ") && !skipCheck {
			// indicates a capture of build output of a package. set the current build package.
			capturedPackage = line[2:]
		} else if capturedPackage != "" && !skipCheck {
			// current line is build failure capture for the current built package
			packageCaptures[capturedPackage] = append(packageCaptures[capturedPackage], line)
		} else if regexSummary.MatchString(line) && !skipCheck {
			// don't store any output after the summary
			seenSummary = true
		} else if !seenSummary && !skipCheck {
			// buffer anything else that we didn't recognize
			buffer = append(buffer, line)
		}

		// Split the test output into Create and Destroy sections
		test := findTest(tests, cur)
		if test == nil {
			continue
		}

		if matches := regexDestroyStart.FindAllString(line, -1); len(matches) > 0 {
			// We are now in the destroy section of the test pass
			isCreate = false
			test.DestroyText = append(test.DestroyText, line)
			//fmt.Println("Found Destroy Text: Set isCreate to False.")
		} else if isCreate == true {
			// add the log line to the CreateText array
			test.CreateText = append(test.CreateText, line)
			//fmt.Printf("Create : %v\n", test.CreateText)
		} else if isCreate == false {
			test.DestroyText = append(test.DestroyText, line)
			//fmt.Printf("Destroy: %v\n", test.DestroyText)
		}
	}

	// no result line found
	report.Packages = append(report.Packages, Package{
		Name:        pkgName,
		Time:        testsTime,
		Tests:       tests,
		CoveragePct: coveragePct,
	})

	return report, nil
}

func processCreateDestroySections(test *Test) *Test {

	var rt ResourceTime
	linesProcessed := 0
	var totalTestTime float64

	for txtIndex, createText := range test.CreateText {
		if linesProcessed == len(test.CreateText)-1 {
			break
		}

		//check to see if it is the DIFF line
		if matches := regexIsDiff.FindStringSubmatch(createText); len(matches) > 0 {

			// We found the diff line, now start looking for Create|Update|Destroy
			for i := txtIndex; i < len(test.CreateText); i++ {
				linesProcessed = i

				if matches := regexIsCreateUpdateOrDestroy.FindStringSubmatch(test.CreateText[i]); len(matches) > 0 {

					// Now I need to see if this Resource time already has an action of this type
					// already in the data structure, if not add it

					rt.Action = addAction(rt.Action, matches[1])

					if len(rt.ResourceName) == 0 {
						rt.ResourceName = matches[2]
					} else {
						rt.ResourceName = fmt.Sprintf("%s, %s", rt.ResourceName, matches[2])
					}

				} else {
					// Also need to be looking for start time here
					if matches := regexIsGraphTypeApply.FindStringSubmatch(test.CreateText[i]); len(matches) > 0 {
						// We are on the start line for the create methods
						// Now parse the start time fron the log line
						startTime, _ := time.Parse(time.RFC3339, convertToRFC3339(test.CreateText[i]))

						// Now we need to look for the end time
						for x := i; x < len(test.CreateText); x++ {
							if matches := regexIsGraphTypePlan.FindStringSubmatch(test.CreateText[x]); len(matches) > 0 {
								endTime, _ := time.Parse(time.RFC3339, convertToRFC3339(test.CreateText[x]))
								rt.Duration = float64(endTime.Sub(startTime))
								test.Steps = append(test.Steps, rt)
								i = x
								rt.Action = nil
								totalTestTime = totalTestTime + float64(rt.Duration)
								test.CreateTime = totalTestTime
								rt.Duration = 0.0 //time.Duration(0.0)
								rt.ResourceName = ""
								break
							}
						}
					}
				}
			}

		} else {
			continue
		}
	}

	// Now look over the destroy text
	var drt ResourceTime
	linesProcessed = 0

	for txtIndex, destroyText := range test.DestroyText {
		if linesProcessed == len(test.DestroyText)-1 {
			break
		}

		//check to see if it is the DIFF line
		if matches := regexIsDiff.FindStringSubmatch(destroyText); len(matches) > 0 {

			// We found the diff line, now start looking for Create|Update|Destroy
			for i := txtIndex; i < len(test.DestroyText); i++ {
				linesProcessed = i
				if matches := regexIsCreateUpdateOrDestroy.FindStringSubmatch(test.DestroyText[i]); len(matches) > 0 {

					// Now I need to see if this Resource time already has an action of this type
					// already in the data structure, if not add it
					drt.Action = addAction(drt.Action, matches[1])

					if len(drt.ResourceName) == 0 {
						drt.ResourceName = matches[2]
					} else {
						drt.ResourceName = fmt.Sprintf("%s, %s", drt.ResourceName, matches[2])
					}
				} else {
					// Also need to be looking for start time here
					if matches := regexIsGraphTypeApply.FindStringSubmatch(test.DestroyText[i]); len(matches) > 0 {
						// We are on the start line for the create methods
						// Now parse the start time fron the log line
						startTime, _ := time.Parse(time.RFC3339, convertToRFC3339(test.DestroyText[i]))

						// Now we need to look for the end time
						for x := i; x < len(test.DestroyText); x++ {
							if matches := regexIsGraphTypeDestroy.FindStringSubmatch(test.DestroyText[x]); len(matches) > 0 {
								endTime, _ := time.Parse(time.RFC3339, convertToRFC3339(test.DestroyText[x]))
								drt.Duration = float64(endTime.Sub(startTime))
								test.CleanUp = append(test.CleanUp, drt)
								totalTestTime = totalTestTime + float64(drt.Duration)
								test.TestOverhead = (test.Time * float64(time.Second)) - totalTestTime
								test.CreateDestroyTime = totalTestTime
								i = x
								break
							}
						}
					}
				}
			}

		} else {

			continue
		}
	}

	Console.WriteLine("------------------------------------------------")
	Console.WriteLine("")
	Console.Write("TEST             : %s\n", test.Name)
	Console.Write("Time             : %v\n", time.Duration(test.Time)*time.Second)
	Console.Write("CreateTime       : %v\n", time.Duration(test.CreateTime))

	if len(test.CleanUp) > 0 {
		Console.Write("DestroyTime      : %v\n", time.Duration(test.CleanUp[0].Duration))
	} else {
		Console.WriteLine("DestroyTime      : 0.0")
	}

	Console.Write("CreateDestroyTime: %v\n", time.Duration(test.CreateDestroyTime))
	Console.Write("Overhead         : %v\n", time.Duration(test.TestOverhead))
	Console.WriteLine("")
	Console.WriteLine("  CREATE STEP:")

	for _, cur := range test.Steps {

		Console.Write("    TestStep      : %v\n", cur.Action)
		Console.Write("    TestStep      : %v\n", cur.ResourceName)
		Console.Write("    TestStep      : %v\n\n", fmt.Sprintf("%.3f", float64(cur.Duration/float64(time.Second))))
	}

	Console.WriteLine("  DESTROY STEP:")

	if len(test.CleanUp) > 0 {
		Console.Write("    DestroyStep   : %v\n", test.CleanUp[0].Action)
		Console.Write("    DestroyStep   : %v\n", test.CleanUp[0].ResourceName)
		Console.Write("    DestroyStep   : %v\n", fmt.Sprintf("%.3f", float64(test.CleanUp[0].Duration/float64(time.Second))))
	} else {
		Console.WriteLine("    DestroyStep   : N/A")
	}
	Console.WriteLine("")

	return test
}

func addAction(actions []ACTION, action string) []ACTION {
	var newAction = actionFromString(action)

	if len(actions) == 0 {
		actions = make([]ACTION, 0)
		actions = append(actions, newAction)
	} else {
		for i := len(actions) - 1; i >= 0; i-- {
			if actions[i] == newAction {
				return actions
			}
		}

		actions = append(actions, newAction)
	}

	return actions
}

func actionFromString(action string) ACTION {

	switch strings.ToLower(action) {
	case "create":
		return CREATE
	case "update":
		return UPDATE
	case "destroy":
		return DESTROY
	default:
		return UNKNOWN
	}
}

func clearCreateDestroyText(test *Test) {
	//fmt.Printf("Clear Create: %s", test.CreateText)
	test.CreateText = nil

	//fmt.Printf("Clear Destroy: %s", test.DestroyText)
	test.DestroyText = nil
}

func parseTime(time string) float64 {
	var t float64
	t, _ = strconv.ParseFloat(time, 64)

	return t
}

func convertToRFC3339(time string) string {
	var rfc3339Str = time
	if matches := regexTimeFormat.FindStringSubmatch(time); len(matches) == 7 {
		rfc3339Str = fmt.Sprintf("%v-%v-%vT%v:%v:%v+08:00",
			matches[1], matches[2], matches[3],
			matches[4], matches[5], matches[6])
	}

	return rfc3339Str
}

func findTest(tests []*Test, name string) *Test {
	for i := len(tests) - 1; i >= 0; i-- {
		if tests[i].Name == name {
			return tests[i]
		}
	}
	return nil
}

// Failures counts the number of failed tests in this report
func (r *Report) Failures() int {
	count := 0

	for _, p := range r.Packages {
		for _, t := range p.Tests {
			if t.Result == FAIL {
				count++
			}
		}
	}

	return count
}

func (action ACTION) String() string {
	actions := [...]string{
		"UNKNOWN",
		"CREATE",
		"UPDATE",
		"DESTROY",
	}

	if action < UNKNOWN || action > DESTROY {
		return "UNKNOWN"
	}

	return actions[action]
}

// Write control the flow of output messages
func (c console) Write(s string, a ...interface{}) {
	if c {
		fmt.Printf(s, a...)
	}
}

// WriteLine control the flow of output messages
func (c console) WriteLine(s string) {
	if c {
		fmt.Println(s)
	}
}
