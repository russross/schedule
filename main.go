package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"time"
)

var (
	workers       = runtime.NumCPU()
	pin           = 93.0
	pindev        = 5.0
	dur           = 5 * time.Minute
	warmup        = 15 * time.Second
	restartLocal  = 30 * time.Second
	restartGlobal = 60 * time.Second
	prefix        = "schedule"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	flag.IntVar(&workers, "workers", workers, "number of concurrent workers")
	flag.Float64Var(&pin, "pin", pin, "percent chance mean that a pin will be honored")
	flag.Float64Var(&pindev, "pindev", pindev, "stddev for how much to vary the pin between attempts")
	flag.DurationVar(&dur, "time", dur, "total time to spend searching")
	flag.DurationVar(&warmup, "warmup", warmup, "time to spend finding best random schedule before refining it")
	flag.DurationVar(&restartLocal, "restartlocal", restartLocal, "restart after this long since finding a local best score")
	flag.DurationVar(&restartGlobal, "restartglobal", restartGlobal, "restart after this long since finding the global best score")
	flag.StringVar(&prefix, "prefix", prefix, "file name prefix (.txt, .html, and .json suffixes)")
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
	if pindev < 0.0 {
		log.Fatalf("pindev must be >= 0")
	}
	if dur <= 0 {
		log.Fatalf("time must be > 0")
	}
	if warmup <= 0 {
		log.Fatalf("warmup time must be > 0")
	}
	if restartLocal <= 0 {
		log.Fatalf("restartlocal time must be > 0")
	}
	if restartGlobal <= 0 {
		log.Fatalf("restartglobal time must be > 0")
	}

	// get the input data
	lines, err := fetchFile(prefix + ".txt")
	if err != nil {
		log.Fatalf("%v", err)
	}

	// parse it
	data, err := Parse(prefix+".txt", lines)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// generate the list of sections and constraints
	sections := data.MakeSectionList()
	log.Printf("starting main search")
	startTime := time.Now()

	//
	// start the main search
	//
	var wg sync.WaitGroup
	var mutex sync.Mutex

	worst := int(1e9)
	baselinePlacements := []Placement{}
	localBestScore, localBestPlacements := worst, []Placement{}
	globalBestScore := worst
	lastImprovement := time.Now()
	successfullAttempts := 0
	failedAttempts := 0

	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			for {
				now := time.Now()
				if time.Since(startTime) > dur {
					break
				}

				mutex.Lock()

				switch {
				// are we in a warmup?
				case len(baselinePlacements) == 0:
					// is it time to move on to refinement?
					if now.Sub(lastImprovement) >= warmup {
						if len(localBestPlacements) == 0 {
							// we did not find any valid schedules
							log.Fatalf("no valid schedule found in warmup period")
						}
						baselinePlacements = localBestPlacements
						lastImprovement = now
						log.Printf("ending warmup")
					}

				// is it time to restart?
				case localBestScore > globalBestScore && now.Sub(lastImprovement) >= restartLocal ||
					localBestScore <= globalBestScore && now.Sub(lastImprovement) >= restartGlobal:
					baselinePlacements = nil
					localBestScore, localBestPlacements = worst, nil
					lastImprovement = now
					log.Printf("restarting")
				}

				base := baselinePlacements
				mutex.Unlock()

				// generate a schedule
				candidate := data.PlaceSections(sections, base)
				if len(candidate) == 0 {
					mutex.Lock()
					failedAttempts++
					mutex.Unlock()
					continue
				}

				// score it
				schedule := data.Score(candidate)

				// see how it compares
				now = time.Now()
				mutex.Lock()
				successfullAttempts++

				if schedule.Badness < globalBestScore {
					// new global best? always keep it
					globalBestScore = schedule.Badness
					localBestScore, localBestPlacements = schedule.Badness, candidate

					if len(baselinePlacements) == 0 {
						// if we are in a warmup, just keep going
						log.Printf("global best of %d found in warmup", schedule.Badness)
					} else {
						// if we are in a refinement period, reset the counter and the baseline
						baselinePlacements = candidate
						lastImprovement = now
						log.Printf("global best of %d found", schedule.Badness)
					}
					data.PrintSchedule(schedule)
				} else if schedule.Badness < localBestScore {
					// new local best?
					switch {
					case len(baselinePlacements) == 0 && len(base) > 0:
						// it was a holdover from before a restart, so discard it

					case len(baselinePlacements) == 0:
						// warmup
						localBestScore, localBestPlacements = schedule.Badness, candidate
						log.Printf("warmup best of %d found (global best is %d)", schedule.Badness, globalBestScore)

					default:
						// refinement
						baselinePlacements = candidate
						localBestScore, localBestPlacements = schedule.Badness, candidate
						lastImprovement = now
						log.Printf("local best of %d found (global best is %d)", schedule.Badness, globalBestScore)
					}
				}

				mutex.Unlock()
			}
			wg.Done()
		}()
	}
	wg.Wait()
	log.Printf("%d successful and %d failed attempts in %v", successfullAttempts, failedAttempts, time.Since(startTime))
}

func (data *InputData) PrintSchedule(schedule Schedule) {
	nameLen := 0
	for _, instructor := range data.Instructors {
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
	for t := range data.Times {
		for r := range data.Rooms {
			cell := schedule.RoomTimes[r][t]
			switch {
			case cell.IsSpillover:
				fmt.Printf("+ %-*s ", nameLen, "")
			default:
				fmt.Printf("+-%s-", hyphens)
			}
		}
		fmt.Println("+")
		for r := range data.Rooms {
			cell := schedule.RoomTimes[r][t]
			switch {
			case cell.Course != nil && !cell.IsSpillover:
				fmt.Printf("| %-*s ", nameLen, cell.Course.Instructor.Name)
			default:
				fmt.Printf("| %-*s ", nameLen, "")
			}
		}
		fmt.Println("|")
		for r := range data.Rooms {
			cell := schedule.RoomTimes[r][t]
			switch {
			case cell.Course != nil && !cell.IsSpillover:
				fmt.Printf("| %-*s ", nameLen, cell.Course.Name)
			default:
				fmt.Printf("| %-*s ", nameLen, "")
			}
		}
		fmt.Println("|")
	}
	for range data.Rooms {
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
