package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.Ltime)

	workers := runtime.NumCPU()
	dur := time.Minute
	pin := 93.0
	pinDev := 5.0
	reSort := 15
	reStart := 30 * time.Second
	reStartBest := 60 * time.Second
	inFile := "input.txt"
	outPrefix := "schedule"

	flag.IntVar(&workers, "workers", workers, "number of concurrent workers")
	flag.Float64Var(&pin, "pin", pin, "percent chance mean that a pin will be honored")
	flag.Float64Var(&pinDev, "pindev", pinDev, "percent chance stddev that a pin will be honored")
	flag.IntVar(&reSort, "sort", reSort, "how often to re-sort sections to be placed")
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

	data := &DataSet{
		Instructors: make(map[string]*Instructor),
		Rooms:       make(map[string]*Room),
		Times:       make(map[string]*Time),

		TagToRooms: make(map[string][]*Room),
		TagToTimes: make(map[string][]*Time),
	}

	// parse everything
	if lines, err := fetchFile(inFile); err != nil {
		log.Fatalf("%v", err)
	} else if err := data.Parse(inFile, lines); err != nil {
		log.Fatalf("%v", err)
	}
	log.Printf("finding the minimum possible number of rooms for each instructor")
	findMinRooms(data.Instructors)
	pristine := NewSearchState(data, pin, pinDev, reSort)

	generation := 0
	var mutex sync.RWMutex

	log.Printf("searching for %v with pins honored at %f%% (stddev %f%%)", dur, pin, pinDev)
	start := time.Now()

	// one goroutine gathers results
	results := make(chan *SearchState, workers*2)
	resultsFinished := make(chan struct{})
	go func() {
		attempts, total, count := 0, 0, 0
		bestScore := -1
		currentScore := -1
		lastReport := start
		lastBest := start
		currentGenIsBest := false
		for result := range results {
			attempts++

			// failed attempt?
			if result.Badness < 0 {
				continue
			}

			// new best for this generation?
			if result.Generation == generation && (currentScore < 0 || result.Badness <= currentScore) {
				if currentScore < 0 || result.Badness < currentScore {
					log.Printf("schedule found with badness %d", result.Badness)
					currentScore = result.Badness
					lastBest = time.Now()
				}
				mutex.Lock()
				rePin(data, result)
				mutex.Unlock()
			}

			// is this an all-time best?
			if bestScore < 0 || result.Badness < bestScore {
				log.Printf("new best score, saving result")
				bestScore = result.Badness
				currentGenIsBest = true

				// save the HTML format
				fp, err := os.Create(outPrefix + ".html")
				if err != nil {
					log.Fatalf("%v", err)
				}
				writeRoomByTime(fp, result)
				fp.Close()

				// save the CSV format
				fp, err = os.Create(outPrefix + ".txt")
				if err != nil {
					log.Fatalf("%v", err)
				}
				save(false, fp, data, result)
				fp.Close()
			}

			if time.Since(lastReport) > time.Minute {
				log.Printf("so far: %d runs in %v, badness score of %d",
					attempts, round(time.Since(start), time.Second), bestScore)
				lastReport = time.Now()
			}

			if currentGenIsBest && time.Since(lastBest) > reStartBest || !currentGenIsBest && time.Since(lastBest) > reStart {
				log.Printf("no improvements for %v, restarting", round(time.Since(lastBest), time.Second))
				lastBest = time.Now()
				currentScore = -1

				mutex.Lock()
				generation++
				unPin(data)
				mutex.Unlock()
				currentGenIsBest = false
			}

			total += result.Badness
			count++
		}

		if count > 0 {
			log.Printf("best schedule found has badness %d", bestScore)
		}
		log.Printf("%d successful runs out of %d attempts with %d generations in %v",
			count, attempts, generation+1, round(time.Since(start), time.Second))
		resultsFinished <- struct{}{}
	}()

	{
		// pin at 100% to establish a baseline
		mutex.Lock()
		state := pristine.Clone()
		state.Generation = generation
		state.PinMean = 100.0
		state.PinStddev = 0.0
		state.Solve()
		state.Complain()
		mutex.Unlock()
		results <- state
	}

	// other goroutines run jobs
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Since(start) < dur {
				mutex.RLock()
				state := pristine.Clone()
				state.Generation = generation
				state.Solve()
				state.Complain()
				mutex.RUnlock()
				results <- state
			}
		}()
	}

	wg.Wait()
	close(results)
	<-resultsFinished
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
