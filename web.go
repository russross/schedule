// +build wasm

package main

import (
	"bufio"
	"fmt"
	"log"
	"strings"
	"syscall/js"
)

const nbsp string = "\u00A0"

var globalInputData *InputData
var verbose bool

func main() {
	log.SetFlags(log.Ltime)
	log.Println("main called")
	js.Global().Get("schedule").Set("setSchedule", js.FuncOf(WasmSetSchedule))
	js.Global().Get("schedule").Set("slotsNeeded", js.FuncOf(WasmSlotsNeeded))
	js.Global().Get("schedule").Set("canonicalOutput", js.FuncOf(WasmCanonicalOutput))

	// run forever
	<-make(chan struct{})
}

// Call with the raw bytes of the schedule.txt file as the only argument.
// Parses the InputData and stores it for future reference.
// Should be called once at startup time.
func WasmSetSchedule(this js.Value, args []js.Value) interface{} {
	switch {
	case len(args) < 1 || len(args) > 2:
		log.Printf("schedule.setSchedule: expected 1 or 2 arguments, found %d", len(args))
		return nil
	case len(args) == 1 && globalInputData == nil:
		log.Printf("schedule.setSchedule: must be called with both schedule.json and schedule.txt on first call")
		return nil
	case len(args) == 2 && globalInputData != nil:
		log.Printf("schedule.setSchedule: must only be called with schedule.json after first call")
		return nil
	}

	// parse main schedule.txt input
	if len(args) == 2 {
		var lines [][]string
		scanner := bufio.NewScanner(strings.NewReader(args[1].String()))
		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			lines = append(lines, fields)
		}
		if err := scanner.Err(); err != nil {
			log.Printf("schedule.setSchedule: scanning schedule.txt input: %v", err)
			return nil
		}

		data, err := Parse("schedule.txt", lines)
		if err != nil {
			log.Printf("schedule.setSchedule: parsing schedule.txt input: %v", err)
			return nil
		}
		globalInputData = data
		log.Println("schedule.setSchedule: schedule.txt ingested and parsed")
	}

	// parse schedule.json and score it
	placements, err := globalInputData.ReadJSON(strings.NewReader(args[0].String()))
	if err != nil {
		log.Printf("schedule.score: parsing schedule.json input: %v", err)
		return nil
	}
	schedule := globalInputData.Score(placements)

	// create the table
	document := js.Global().Get("document")
	appendElement := func(parent js.Value, element string) js.Value {
		elt := document.Call("createElement", element)
		parent.Call("appendChild", elt)
		return elt
	}
	appendText := func(parent js.Value, text string) js.Value {
		elt := document.Call("createTextNode", text)
		parent.Call("appendChild", elt)
		return elt
	}
	tbody := document.Call("getElementById", "grid")
	badness := document.Call("getElementById", "badness")
	problems := document.Call("getElementById", "problems")

	// clear the tbody
	for {
		child := tbody.Get("firstChild")
		if child.Type() == js.TypeNull {
			break
		}
		tbody.Call("removeChild", child)
	}

	// clear the badness message
	for {
		child := badness.Get("firstChild")
		if child.Type() == js.TypeNull {
			break
		}
		badness.Call("removeChild", child)
	}

	// clear the problems list
	for {
		child := problems.Get("firstChild")
		if child.Type() == js.TypeNull {
			break
		}
		problems.Call("removeChild", child)
	}

	// make the header row with room names
	{
		tr := appendElement(tbody, "tr")
		td := appendElement(tr, "td")
		appendText(td, nbsp)

		for _, room := range globalInputData.Rooms {
			td := appendElement(tr, "td")
			appendText(td, room.Name)
		}
	}

	// create one row per time slot
	for ti, t := range globalInputData.Times {
		tr := appendElement(tbody, "tr")

		// first cell is the time
		td := appendElement(tr, "td")
		appendText(td, t.Name)

		// one cell per room
		for ri, r := range globalInputData.Rooms {
			cell := schedule.RoomTimes[ri][ti]

			switch {
			case cell.IsSpillover:
				// skip this td
			case cell.Course == nil:
				slots := 1
				cur := t
				for cur.Next != nil && schedule.RoomTimes[ri][ti+slots].Course == nil {
					cur = cur.Next
					slots++
				}
				td := appendElement(tr, "td")
				td.Call("setAttribute", "data-time-name", t.Name)
				td.Call("setAttribute", "data-room-name", r.Name)
				td.Call("setAttribute", "data-slots-available", slots)
				appendText(td, nbsp)
			default:
				var index int
				for index = 0; index < len(cell.Course.Instructors[0].Courses); index++ {
					if cell.Course == cell.Course.Instructors[0].Courses[index] {
						break
					}
				}
				td := appendElement(tr, "td")
				td.Call("setAttribute", "data-time-name", t.Name)
				td.Call("setAttribute", "data-instructor-name", cell.Course.Instructors[0].Name)
				td.Call("setAttribute", "data-instructor-course-index", index)
				td.Call("setAttribute", "data-slots-available", 0)
				td.Call("setAttribute", "draggable", "true")
				slots := cell.Course.SlotsNeeded(t)
				if slots > 1 {
					td.Call("setAttribute", "rowspan", slots)
				}
				instructorName := cell.Course.Instructors[0].Name
				if len(cell.Course.Instructors) > 1 {
					instructorName += "+"
				}
				appendText(td, instructorName)
				appendElement(td, "br")
				appendText(td, cell.Course.Name)
			}
		}
	}

	appendText(badness, fmt.Sprintf("Total badness %d with the following known problems:", schedule.Badness))

	for _, problem := range schedule.Problems {
		appendText(appendElement(problems, "li"), problem)
	}

	log.Printf("schedule.setSchedule: schedule rendered")
	js.Global().Get("schedule").Call("setupHover")
	js.Global().Get("schedule").Call("setupDragDrop")

	return nil
}

func WasmSlotsNeeded(this js.Value, args []js.Value) interface{} {
	if len(args) != 4 {
		log.Printf("schedule.slotsNeeded: expected 3 arguments, found %d", len(args))
		return nil
	}
	if globalInputData == nil {
		log.Printf("schedule.slotsNeeded: schedule.txt must be ingested before calling score")
		return nil
	}

	instructorName := args[0].String()
	courseIndex := args[1].Int()
	timeName := args[2].String()
	callback := args[3]

	// find the instructor object
	for _, instructor := range globalInputData.Instructors {
		if instructor.Name == instructorName {
			// get the course
			if courseIndex < 0 || courseIndex >= len(instructor.Courses) {
				log.Printf("schedule.slotsNeeded: invalid course index %d for %s", courseIndex, instructorName)
				return nil
			}
			course := instructor.Courses[courseIndex]

			// find the time object
			for _, t := range globalInputData.Times {
				if t.Name == timeName {
					slots := course.SlotsNeeded(t)
					callback.Invoke(slots)
					return nil
				}
			}
			log.Printf("schedule.slotsNeeded: invalid time %q", timeName)
			return nil
		}
	}
	log.Printf("schedule.slotsNeeded: invalid instructor %q", instructorName)

	return nil
}

func WasmCanonicalOutput(this js.Value, args []js.Value) interface{} {
	if len(args) != 2 {
		log.Printf("schedule.canonicalOutput: expected 2 arguments, found %d", len(args))
		return nil
	}
	if globalInputData == nil {
		log.Printf("schedule.canonicalOutput: schedule.txt must be ingested before calling score")
		return nil
	}

	raw := args[0].String()
	callback := args[1]

	placements, err := globalInputData.ReadJSON(strings.NewReader(raw))
	if err != nil {
		log.Printf("schedule.canonicalOutput: reading input JSON: %v", err)
		return nil
	}

	builder := new(strings.Builder)
	err = globalInputData.WriteJSON(builder, placements)
	if err != nil {
		log.Printf("schedule.canonicalOutput: writing JSON: %v", err)
		return nil
	}

	callback.Invoke(builder.String())

	return nil
}
