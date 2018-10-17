package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	workers       = runtime.NumCPU()
	pin           = 95.0
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

	reportInterval = time.Minute

	ModeWarmup int = iota
	ModeLocalBest
	ModeGlobalBest
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	cmdSchedule := &cobra.Command{
		Use:   "schedule",
		Short: "Course schedule generator",
		Long: "A tool to generate course schedules while optimizing curriculum conflicts\n" +
			"and instructor schedules\n" +
			"by Russ Ross <russ@russross.com>",
	}

	cmdGen := &cobra.Command{
		Use:   "gen",
		Short: "generate and optimize a schedule",
		Run:   CommandGen,
	}
	cmdGen.Flags().IntVar(&workers, "workers", workers, "number of concurrent workers")
	cmdGen.Flags().StringVar(&prefix, "prefix", prefix, "file name prefix (.txt, and .json suffixes will be added)")
	cmdGen.Flags().Float64VarP(&pin, "pin", "p", pin, "the mean percentage that a prior placement will be kept")
	cmdGen.Flags().Float64VarP(&pindev, "pindev", "d", pindev, "the stddev for how much to vary the pin between attempts")
	cmdGen.Flags().DurationVarP(&dur, "time", "t", dur, "total time to spend searching")
	cmdGen.Flags().DurationVarP(&warmup, "warmup", "w", warmup, "time to spend finding best random schedule before refining it")
	cmdGen.Flags().DurationVarP(&restartLocal, "restartlocal", "l", restartLocal, "restart after this long since finding a local best score")
	cmdGen.Flags().DurationVarP(&restartGlobal, "restartglobal", "g", restartGlobal, "restart after this long since finding the global best score")
	cmdSchedule.AddCommand(cmdGen)

	cmdSwap := &cobra.Command{
		Use:   "swap",
		Short: "optimize a schedule by swapping courses",
		Run:   CommandSwap,
	}
	cmdSwap.Flags().IntVar(&workers, "workers", workers, "number of concurrent workers")
	cmdSwap.Flags().StringVar(&prefix, "prefix", prefix, "file name prefix (.txt, and .json suffixes will be added)")
	cmdSwap.Flags().IntVarP(&maxSwapDepth, "max", "m", maxSwapDepth, "maximum number of swaps to attempt")
	cmdSchedule.AddCommand(cmdSwap)

	cmdScore := &cobra.Command{
		Use:   "score",
		Short: "score and display the current schedule",
		Run:   CommandScore,
	}
	cmdScore.Flags().StringVar(&prefix, "prefix", prefix, "file name prefix (.txt, and .json suffixes will be added)")
	cmdSchedule.AddCommand(cmdScore)

	cmdSchedule.Execute()
}

func CommandGen(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		log.Fatalf("unknown option: %s", strings.Join(args, " "))
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
	lastReport := startTime

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
		go func(workerN int) {
			for {
				now := time.Now()
				if time.Since(startTime) > dur {
					break
				}

				mutex.Lock()
				if time.Since(lastReport) >= reportInterval {
					lastReport = lastReport.Add(reportInterval)
					log.Printf("so far: %d runs in %v, badness score of %d",
						successfullAttempts+failedAttempts,
						lastReport.Sub(startTime),
						globalBest.Badness)
				}

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

				// is it time to restart from local or global best?
				case mode == ModeLocalBest && now.Sub(lastImprovement) >= restartLocal:
					fallthrough
				case mode == ModeGlobalBest && now.Sub(lastImprovement) >= restartGlobal:
					baseline = Schedule{Badness: worst}
					localBest = Schedule{Badness: worst}
					lastImprovement = now
					log.Printf("restarting")
					mode = ModeWarmup
				}

				base := baseline.Placements
				mutex.Unlock()

				// the pin value to use for this round
				var localPin float64
				switch {
				case pin >= 100.0:
					localPin = 100.0
				case pin <= 0.0:
					localPin = 0.0
				default:
					localPin = -1.0
					for localPin >= 100.0 || localPin < 0.0 {
						localPin = rand.NormFloat64()*pindev + pin
					}
				}

				// generate a schedule
				candidate := data.PlaceSections(sections, base, localPin)
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
						log.Printf("global best of %d found (pin %.1f)", schedule.Badness, localPin)
						mode = ModeGlobalBest
					}
					data.PrintSchedule(schedule)

					// write schedule to .json file
					writeJsonFile(data, prefix+".json", candidate)
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
						log.Printf("local best of %d found (pin %.1f, global best is %d)", schedule.Badness, localPin, globalBest.Badness)
					}
				}

				mutex.Unlock()
			}
			wg.Done()
		}(worker)
	}
	wg.Wait()
	log.Printf("%d successful and %d failed attempts in %v", successfullAttempts, failedAttempts, time.Since(startTime))
}

func CommandSwap(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		log.Fatalf("unknown option: %s", strings.Join(args, " "))
	}

	if workers < 1 {
		log.Fatalf("workers must be >= 1")
	}
	if maxSwapDepth < 1 {
		log.Fatalf("max must be >= 1")
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

	// read the starting schedule
	fp, err := os.Open(prefix + ".json")
	if err != nil {
		if err == os.ErrNotExist {
			log.Fatalf("the list of course placements must be in %s.json", prefix)
		} else {
			log.Fatalf("opening %s: %v", prefix+".json", err)
		}
	}
	placements, err := data.ReadJSON(fp)
	if err != nil {
		log.Fatalf("reading %s: %v", prefix+".json", err)
	}
	if err = fp.Close(); err != nil {
		log.Fatalf("closing %s: %v", prefix+".json", err)
	}

	globalBest := data.Score(placements)
	newBest := globalBest
	repeat := true

	for repeat {
		repeat = false
		log.Printf("starting a swap search with maximum of %d swaps", maxSwapDepth)
		log.Printf("trying to beat a badness score of %d", globalBest.Badness)
		start := time.Now()

		var wg sync.WaitGroup
		var mutex sync.Mutex

		nextToDisplace := 0

		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				for {
					mutex.Lock()

					// nothing to do?
					if nextToDisplace >= len(sections) {
						mutex.Unlock()
						break
					}

					n := nextToDisplace
					nextToDisplace++
					mutex.Unlock()

					best := data.SearchSwaps(sections, globalBest, maxSwapDepth, n)

					mutex.Lock()
					if best.Badness < newBest.Badness {
						log.Printf("swapping found a new best score of %d", best.Badness)
						newBest = best
						repeat = true
						writeJsonFile(data, prefix+".json", best.Placements)
						data.PrintSchedule(newBest)
					}
					mutex.Unlock()
				}
				wg.Done()
			}()
		}
		wg.Wait()
		log.Printf("swapping finished in %v", time.Since(start))

		if newBest.Badness < globalBest.Badness {
			globalBest = newBest
			log.Printf("swapping improved the score; starting over with new schedule as starting point")
		}
	}
}

func CommandScore(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		log.Fatalf("unknown option: %s", strings.Join(args, " "))
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

	// read the schedule
	fp, err := os.Open(prefix + ".json")
	if err != nil {
		if err == os.ErrNotExist {
			log.Fatalf("the list of course placements must be in %s.json", prefix)
		} else {
			log.Fatalf("opening %s: %v", prefix+".json", err)
		}
	}
	placements, err := data.ReadJSON(fp)
	if err != nil {
		log.Fatalf("reading %s: %v", prefix+".json", err)
	}
	if err = fp.Close(); err != nil {
		log.Fatalf("closing %s: %v", prefix+".json", err)
	}

	schedule := data.Score(placements)
	data.PrintSchedule(schedule)
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
	roomLen := 0
	for _, r := range data.Rooms {
		if len(r.Name) > roomLen {
			roomLen = len(r.Name)
		}
	}
	if roomLen > nameLen {
		nameLen = roomLen
	}
	timeLen := 0
	for _, t := range data.Times {
		if len(t.Name) > timeLen {
			timeLen = len(t.Name)
		}
	}

	hyphens := ""
	dots := ""
	for i := 0; i < nameLen; i++ {
		hyphens += "-"
		dots += "."
	}
	fmt.Printf("%*s ", timeLen, "")
	for _, r := range data.Rooms {
		pad := (nameLen - roomLen) / 2
		fmt.Printf("  %*s%-*s ", pad, "", nameLen-pad, r.Name)
	}
	fmt.Println()
	for t, telt := range data.Times {
		fmt.Printf("%*s ", timeLen, "")
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
		fmt.Printf("%*s ", timeLen, telt.Name)
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
		fmt.Printf("%*s ", timeLen, "")
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
	fmt.Printf("%*s ", timeLen, "")
	for range data.Rooms {
		fmt.Printf("+-%s-", hyphens)
	}
	fmt.Println("+")
	fmt.Println()
	fmt.Printf("Total badness %d with the following known problems:\n", schedule.Badness)
	for _, msg := range schedule.Problems {
		fmt.Println("* " + msg)
	}
}

func fetchFile(filename string) ([][]string, error) {
	var lines [][]string

	var reader io.Reader
	isCsv := false
	if strings.HasPrefix(filename, "http:") || strings.HasPrefix(filename, "https:") {
		const docsSuffix = "/edit?usp=sharing"
		if strings.HasSuffix(filename, docsSuffix) {
			filename = filename[:len(filename)-len(docsSuffix)] + "/export?format=csv"
			isCsv = true
		}
		log.Printf("downloading input URL %s", filename)
		res, err := http.Get(filename)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		reader = res.Body
	} else {
		log.Printf("reading input file %s", filename)
		fp, err := os.Open(filename)
		if err != nil {
			return nil, err
		}
		defer fp.Close()
		reader = fp
		isCsv = strings.HasSuffix(filename, ".csv")
	}

	if isCsv {
		buf := bufio.NewReader(reader)
		reader := csv.NewReader(buf)
		for {
			record, err := reader.Read()
			if err != nil {
				if err != io.EOF {
					return nil, err
				}
				break
			}
			lines = append(lines, record)
		}
	} else {
		// get a line reader
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			lines = append(lines, fields)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return lines, nil
}

func writeJsonFile(data *InputData, filename string, placements []Placement) {
	tmpFile := filename + ".tmp"
	fp, err := os.Create(tmpFile)
	if err != nil {
		log.Fatalf("creating %s: %v", tmpFile, err)
	}
	if err = data.WriteJSON(fp, placements); err != nil {
		log.Fatalf("writing %s: %v", tmpFile, err)
	}
	if err = fp.Close(); err != nil {
		log.Fatalf("closing %s: %v", tmpFile, err)
	}
	if err = os.Rename(tmpFile, filename); err != nil {
		log.Fatalf("renaming %s to %s: %v", tmpFile, filename, err)
	}

}
