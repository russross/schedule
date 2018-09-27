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
	maxSwapDepth  = 2
	prefix        = "schedule"
)

const (
	worst int = 1e9

	ModeWarmup int = iota
	ModeLocalBest
	ModeGlobalBest
	ModeClimbing
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
	flag.IntVar(&maxSwapDepth, "swaps", maxSwapDepth, "maximum number of swaps to perform when trying to improve a global best score")
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

	mode := ModeWarmup
	baseline := Schedule{Badness: worst}
	localBest := Schedule{Badness: worst}
	globalBest := Schedule{Badness: worst}
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
				case mode == ModeWarmup:
					// is it time to move on to refinement?
					if now.Sub(lastImprovement) >= warmup {
						if len(localBest.Placements) == 0 {
							// we did not find any valid schedules
							log.Fatalf("no valid schedule found in warmup period")
						}
						baseline = localBest
						lastImprovement = now
						log.Printf("ending warmup")
						mode = ModeLocalBest
					}

				// is it time to restart from local best?
				case mode == ModeLocalBest && now.Sub(lastImprovement) >= restartLocal:
					baseline = Schedule{Badness: worst}
					localBest = Schedule{Badness: worst}
					lastImprovement = now
					log.Printf("restarting")
					mode = ModeWarmup

				// is it time to try a swapping search on a global best?
				case mode == ModeGlobalBest && now.Sub(lastImprovement) >= restartGlobal:
					log.Printf("starting a swap search with maximum of %d swaps", maxSwapDepth)
					start := time.Now()
					best := data.SearchSwaps(maxSwapDepth, globalBest, sections)
					now = time.Now()
					if best.Badness < globalBest.Badness {
						log.Printf("swap search improved global best to %d in %v", best.Badness, now.Sub(start))
						data.PrintSchedule(best)
						globalBest = best
						localBest = best
						baseline = best
						lastImprovement = now
					} else {
						log.Printf("swap search found no improvements in %v, restarting", now.Sub(start))
						baseline = Schedule{Badness: worst}
						localBest = Schedule{Badness: worst}
						lastImprovement = now
						mode = ModeWarmup
					}
				}

				base := baseline.Placements
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

				if schedule.Badness < globalBest.Badness {
					// new global best? always keep it
					globalBest = schedule
					localBest = schedule

					if mode == ModeWarmup {
						// if we are in a warmup, just keep going
						log.Printf("global best of %d found in warmup", schedule.Badness)
					} else {
						// if we are in a refinement period, reset the counter and the baseline
						baseline = schedule
						lastImprovement = now
						log.Printf("global best of %d found", schedule.Badness)
						mode = ModeGlobalBest
					}
					data.PrintSchedule(schedule)
				} else if schedule.Badness < localBest.Badness {
					// new local best?
					switch {
					case mode == ModeWarmup && len(base) > 0:
						// it was a holdover from before a restart, so discard it

					case mode == ModeWarmup:
						localBest = schedule
						log.Printf("warmup best of %d found (global best is %d)", schedule.Badness, globalBest.Badness)

					default:
						// refinement
						baseline = schedule
						localBest = schedule
						lastImprovement = now
						log.Printf("local best of %d found (global best is %d)", schedule.Badness, globalBest.Badness)
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
