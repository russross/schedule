package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"time"
)

var (
	pin    float64
	pinDev float64
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	workers := runtime.NumCPU()
	dur := time.Minute
	pin := 93.0
	pinDev := 5.0
	reStart := 30 * time.Second
	reStartBest := 60 * time.Second
	inFile := "input.txt"
	outPrefix := "schedule"

	flag.IntVar(&workers, "workers", workers, "number of concurrent workers")
	flag.Float64Var(&pin, "pin", pin, "percent chance mean that a pin will be honored")
	flag.Float64Var(&pinDev, "pindev", pinDev, "percent chance stddev that a pin will be honored")
	flag.DurationVar(&dur, "time", dur, "total time to spend searching")
	flag.DurationVar(&reStart, "restart", reStart, "restart after this long since finding a local best score")
	flag.DurationVar(&reStartBest, "restartbest", reStartBest, "restart after this long since finding the best so far")
	flag.StringVar(&inFile, "in", inFile, "input file name")
	flag.StringVar(&outPrefix, "out", outPrefix, "output file prefix (.txt and .html suffixes)")
	flag.Parse()
	if flag.NArg() != 0 {
		flag.PrintDefaults()
		log.Fatalf("Usage: %s [options]", os.Args[0])
	}
	if workers < 1 {
		log.Fatalf("workers must be >= 1")
	}
	if pin < 0.0 || pin > 100.0 {
		log.Fatalf("pin must be between 0 and 100")
	}
	if pinDev < 0.0 {
		log.Fatalf("pindev must be >= 0")
	}
	if dur <= 0 {
		log.Fatalf("time must be > 0")
	}

	// get the input data
	lines, err := fetchFile(inFile)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// parse it
	inputData, err := Parse(inFile, lines)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// generate the list of sections and constraints
	sections := inputData.MakeSectionList()

	// generate a schedule
	var placements []Placement

	for placements == nil {
		placements = inputData.PlaceSections(sections, nil)
	}
	schedule := inputData.Score(placements)

	nameLen := 0
	for _, instructor := range inputData.Instructors {
		if len(instructor.Name) > nameLen {
			nameLen = len(instructor.Name)
		}
		for _, course := range instructor.Courses {
			if len(course.Name) > nameLen {
				nameLen = len(course.Name)
			}
		}
	}

	hyphens := ""
	dots := ""
	for i := 0; i < nameLen; i++ {
		hyphens += "-"
		dots += "."
	}
	for t := range inputData.Times {
		for range inputData.Rooms {
			fmt.Printf("+-%s-", hyphens)
		}
		fmt.Println("+")
		for r := range inputData.Rooms {
			cell := schedule.RoomTimes[r][t]
			switch {
			case cell.IsSpillover:
				fmt.Printf("| %s ", dots)
			case cell.Course != nil:
				fmt.Printf("| %*s ", nameLen, cell.Course.Instructor.Name)
			default:
				fmt.Printf("| %*s ", nameLen, "")
			}
		}
		fmt.Println("|")
		for r := range inputData.Rooms {
			cell := schedule.RoomTimes[r][t]
			switch {
			case cell.IsSpillover:
				fmt.Printf("| %s ", dots)
			case cell.Course != nil:
				fmt.Printf("| %*s ", nameLen, cell.Course.Name)
			default:
				fmt.Printf("| %*s ", nameLen, "")
			}
		}
		fmt.Println("|")
	}
	for range inputData.Rooms {
		fmt.Printf("+-%s-", hyphens)
	}
	fmt.Println("+")
	fmt.Println()
	fmt.Println("Known problems:")
	for _, msg := range schedule.Problems {
		fmt.Println("* " + msg)
	}
	fmt.Println()
	fmt.Printf("Total badness %d\n", schedule.Badness)
}
