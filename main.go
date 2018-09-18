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
	placements := inputData.PlaceSections(sections, nil)
	if placements == nil {
		return
	}
	//sortSchedule(placements)
	schedule := inputData.MakeSchedule(placements)

	instructorLen := 0
	courseLen := 0
	for _, instructor := range inputData.Instructors {
		if len(instructor.Name) > instructorLen {
			instructorLen = len(instructor.Name)
		}
		for _, course := range instructor.Courses {
			if len(course.Name) > courseLen {
				courseLen = len(course.Name)
			}
		}
	}

	size := instructorLen
	if courseLen > size {
		size = courseLen
	}
	hyphens := ""
	for i := 0; i < size; i++ {
		hyphens += "-"
	}
	for _, row := range schedule.RoomTimes {
		for range row {
			fmt.Printf("+--%s--", hyphens)
		}
		fmt.Println("+")
		for _, col := range row {
			if col.IsSpillover {
				fmt.Printf("| (%*s) ", size, col.Instructor)
			} else {
				fmt.Printf("|  %*s  ", size, col.Instructor)
			}
		}
		fmt.Println("|")
		for _, col := range row {
			if col.IsSpillover {
				fmt.Printf("| (%*s) ", size, col.Course)
			} else {
				fmt.Printf("|  %*s  ", size, col.Course)
			}
		}
		fmt.Println("|")
	}
	for range schedule.RoomTimes[0] {
		fmt.Printf("+--%s--", hyphens)
	}
	fmt.Println("+")
}
