package main

import (
	"log"
	"math/rand"
	"sort"
)

// a Section is used during schedule creation
type Section struct {
	Course    *Course
	RoomTimes [][]int
	Tickets   int
	Count     int
}

// A Placement represents a course assigned to a room and time
type Placement struct {
	Course *Course
	Room   int
	Time   int
}

// A Cell is one entry in a grid of course room-times.
type Cell struct {
	Course      *Course
	IsSpillover bool
}

// MakeSectionList forms a list of sections in order from most- to least-constrained.
// The list it returns is read-only and only its clones can be modified.
func (data *InputData) MakeSectionList() []*Section {
	var sections []*Section
	for _, instructor := range data.Instructors {
		for _, course := range instructor.Courses {
			section := &Section{
				Course:    course,
				RoomTimes: make([][]int, len(data.Rooms)),
				Tickets:   0,
				Count:     0,
			}
			for i := range section.RoomTimes {
				section.RoomTimes[i] = make([]int, len(data.Times))
			}
			sections = append(sections, section)

			// in order for a time slot to be suitable:
			// * the start slot must be okay for the course OR the course must not specify times
			// * all slots the course occupies must be okay for the instructor
			//
			// the badness is the sum of the worst badness value from each slot used (capped at 99)
			var courseTimes []int
		timeLoop:
			for i := range data.Times {
				// the course must either explictly allow this start time, or have no time preferences
				if len(course.Times) > 0 && course.Times[i] < 0 {
					courseTimes = append(courseTimes, -1)
					continue timeLoop
				}

				// there must be enough slots starting at this time
				// and the instructor must be available for all of them
				slotsNeeded := course.SlotsNeeded(data.Times[i])
				if i+slotsNeeded > len(data.Times) {
					// this would run past the last time slot that exists
					courseTimes = append(courseTimes, -1)
					continue timeLoop
				}
				badness := 0
				for j := 0; j < slotsNeeded; j++ {
					if j+1 < slotsNeeded && data.Times[i+j].Next != data.Times[i+j+1] {
						// not enough time slots in a row to accomodate this course
						courseTimes = append(courseTimes, -1)
						continue timeLoop
					}
					if instructor.Times[i+j] < 0 {
						// the instructor cannot teach at this time
						courseTimes = append(courseTimes, -1)
						continue timeLoop
					}
					badness += instructor.Times[i+j]
				}

				// badness caps at 99
				if badness > 99 {
					badness = 99
				}

				// which is worse: the course preferences for starting this course at this time
				// or instructor preferences for teaching in all of the required slots
				if len(course.Times) > 0 && course.Times[i] > badness {
					badness = course.Times[i]
				}

				courseTimes = append(courseTimes, badness)
			}

			// fill in the badness score for each possible room..
			for roomIndex := 0; roomIndex < len(data.Rooms); roomIndex++ {
				// .. at each possible time
				for timeIndex := 0; timeIndex < len(data.Times); timeIndex++ {
					var badness int
					switch {
					case course.Rooms[roomIndex] < 0 || courseTimes[timeIndex] < 0:
						badness = -1
					case course.Rooms[roomIndex] >= courseTimes[timeIndex]:
						badness = course.Rooms[roomIndex]
					default:
						badness = courseTimes[timeIndex]
					}
					section.RoomTimes[roomIndex][timeIndex] = badness
					if badness >= 0 {
						section.Tickets += 100 - badness
						section.Count++
					}
				}
			}

			// it must be possible to place the section somewhere
			if section.Tickets == 0 || section.Count == 0 {
				log.Fatalf("no valid room/time combinations found for %s taught by %s", course.Name, instructor.Name)
			}
		}
	}

	// sort from most to least constrained
	sort.Slice(sections, func(a, b int) bool {
		return sections[a].Count < sections[b].Count
	})

	return sections
}

func CloneSectionList(original []*Section) []*Section {
	var clone []*Section
	for _, section := range original {
		roomTimes := make([][]int, len(section.RoomTimes))
		for i, times := range section.RoomTimes {
			roomTimes[i] = make([]int, len(times))
			copy(roomTimes[i], times)
		}
		sectionCopy := &Section{
			Course:    section.Course,
			RoomTimes: roomTimes,
			Tickets:   section.Tickets,
			Count:     section.Count,
		}
		clone = append(clone, sectionCopy)
	}
	return clone
}

func (data *InputData) PlaceSections(readOnlySectionList []*Section, oldPlacementList []Placement, localPin float64, weightedLottery bool) []Placement {
	// the schedule we are creating
	var schedule []Placement

	// get the list of sections to place
	sections := CloneSectionList(readOnlySectionList)

	// make it easy to find where the section for a given course is in our section list
	sectionIndex := make(map[*Course]int)
	for i, section := range sections {
		sectionIndex[section.Course] = i
	}

	// make it easy to find the placement of a course in the schedule we are trying to improve
	oldSchedule := make(map[*Course]Placement)
	for _, placement := range oldPlacementList {
		oldSchedule[placement.Course] = placement
	}

	// place the sections one at a time, starting with the most constrained
	for sectionIndex := 0; sectionIndex < len(sections); sectionIndex++ {
		section := sections[sectionIndex]
		r, t := -1, -1

		// should we place this section where it was in the old schedule?
		if oldPlacement, present := oldSchedule[section.Course]; present {
			// we have an old placement to work with
			if section.RoomTimes[oldPlacement.Room][oldPlacement.Time] >= 0 {
				// its old placement is at an available time
				if rand.Float64()*100.0 < localPin {
					// the dice roll says we should keep it here
					r, t = oldPlacement.Room, oldPlacement.Time
				}
			}
		}

		// do we need to run a lottery?
		if r < 0 && t < 0 {
			ticketMax := section.Tickets
			if !weightedLottery {
				ticketMax = section.Count
			}
			ticket := rand.Intn(ticketMax)
		lotteryLoop:
			for room, times := range section.RoomTimes {
				for time, badness := range times {
					if badness < 0 {
						continue
					}
					if weightedLottery {
						ticket -= (100 - badness)
					} else {
						ticket--
					}
					if ticket < 0 {
						r, t = room, time
						break lotteryLoop
					}
				}
			}
		}

		// we must have a room and time by now
		if r < 0 || t < 0 {
			log.Fatalf("search failed to find a placement for %s taught by %s",
				section.Course.Name, section.Course.Instructor.Name)
		}

		// record the placement
		schedule = append(schedule, Placement{Course: section.Course, Room: r, Time: t})

		// update all remaining unplaced sections
		slots := section.Course.SlotsNeeded(data.Times[t])
		for otherIndex := sectionIndex + 1; otherIndex < len(sections); otherIndex++ {
			other := sections[otherIndex]

			// block out this room/time for all sections
			for i := 0; i < slots; i++ {
				other.BlockRoomTime(r, t+i, -1, data.Times)
			}

			// block out this time in all rooms for the same instructor
			if other.Course.Instructor == section.Course.Instructor {
				for room := range data.Rooms {
					for i := 0; i < slots; i++ {
						other.BlockRoomTime(room, t+i, -1, data.Times)
					}
				}
			}

			// update badness in all rooms at this time for sections with conflicts
			if badness, present := section.Course.Conflicts[other.Course]; present {
				for room := range data.Rooms {
					for i := 0; i < slots; i++ {
						other.BlockRoomTime(room, t+i, badness, data.Times)
					}
				}
			}

			// did this make the schedule impossible?
			if other.Tickets <= 0 || other.Count <= 0 {
				/*
					log.Printf("placing %s %s at %s in %s made placing %s %s impossible",
						section.Course.Instructor.Name, section.Course.Name,
						data.Times[t].Name, data.Rooms[r].Name,
						other.Course.Instructor.Name, other.Course.Name)
				*/
				return nil
			}

			// update this section's placement priority based on the new ticket count
			for i := otherIndex - 1; i >= sectionIndex+1 && sections[i+1].Count < sections[i].Count; i-- {
				sections[i+1], sections[i] = sections[i], sections[i+1]
			}
		}
	}

	return schedule
}

func (section *Section) BlockRoomTime(r, t, badness int, times []*Time) {
	slots := section.Course.SlotsNeeded(times[t])
	for i := 0; i < slots && t-i >= 0; i++ {
		if i > 0 && times[t-i].Next != times[t-i+1] {
			break
		}

		old := section.RoomTimes[r][t-i]
		if old >= 0 && (badness < 0 || badness > old) {
			section.RoomTimes[r][t-i] = badness
			section.Tickets -= 100 - old
			section.Count--
			if badness >= 0 {
				section.Tickets += 100 - badness
				section.Count++
			}
		}
	}
}

// sort a schedule by instructor, course
func sortPlacements(placements []Placement) {
	sort.Slice(placements, func(a, b int) bool {
		if placements[a].Course.Instructor != placements[b].Course.Instructor {
			return placements[a].Course.Instructor.Name < placements[b].Course.Instructor.Name
		}
		var ai, bi int
		for ai = 0; ai < len(placements[a].Course.Instructor.Courses); ai++ {
			if placements[a].Course.Instructor.Courses[ai] == placements[a].Course {
				break
			}
		}
		for bi = 0; bi < len(placements[b].Course.Instructor.Courses); bi++ {
			if placements[b].Course.Instructor.Courses[bi] == placements[b].Course {
				break
			}
		}
		return ai < bi
	})
}

func (data *InputData) MakeGrid(placements []Placement) [][]Cell {
	roomTimes := make([][]Cell, len(data.Rooms))
	for i := range roomTimes {
		roomTimes[i] = make([]Cell, len(data.Times))
	}

	for _, placement := range placements {
		slots := placement.Course.SlotsNeeded(data.Times[placement.Time])
		for i := 0; i < slots; i++ {
			if roomTimes[placement.Room][placement.Time+i].Course != nil {
				log.Fatalf("%s %s cannot be scheduled at %s in %s because that slot is already used by %s %s",
					placement.Course.Instructor.Name, placement.Course.Name,
					data.Times[placement.Time].Name, data.Rooms[placement.Room].Name,
					roomTimes[placement.Room][placement.Time+i].Course.Instructor.Name,
					roomTimes[placement.Room][placement.Time+i].Course.Name)
			}
			roomTimes[placement.Room][placement.Time+i].Course = placement.Course
			if i > 0 {
				roomTimes[placement.Room][placement.Time+i].IsSpillover = true
			}
		}
	}

	return roomTimes
}

func (data *InputData) SearchSwaps(sections []*Section, baseline Schedule, maxDepth int, placementIndex int) Schedule {
	// clone the schedule so we can modify it as we search
	working := baseline.Clone()
	best := Schedule{Badness: Impossible}
	courseToSection := make(map[*Course]*Section)
	for _, section := range sections {
		courseToSection[section.Course] = section
	}
	courseToPlacementIndex := make(map[*Course]int)
	for i, placement := range working.Placements {
		courseToPlacementIndex[placement.Course] = i
	}

	// each course that is not currently placed/has been moved
	var displaced []Placement
	var replaced []*Course

	// helper functions
	removeFromMatrix := func(p Placement) {
		slots := p.Course.SlotsNeeded(data.Times[p.Time])
		for i := 0; i < slots; i++ {
			if working.RoomTimes[p.Room][p.Time+i].Course != p.Course {
				panic("removeFromMatrix asked to remove course that was not in expected place")
			}
			working.RoomTimes[p.Room][p.Time+i] = Cell{}
		}
	}

	addToMatrix := func(p Placement) {
		slots := p.Course.SlotsNeeded(data.Times[p.Time])
		for i := 0; i < slots; i++ {
			if working.RoomTimes[p.Room][p.Time+i].Course != nil {
				panic("addToMatrix asked to add course on top of existing course")
			}
			working.RoomTimes[p.Room][p.Time+i] = Cell{Course: p.Course, IsSpillover: i > 0}
		}
	}

	// the main recursive search function
	// it returns with, working, displaced, and replaced are restored to
	// the state they were in when it was called
	// best will have a clone of any improved schedule it finds
	var search func(int)
	search = func(depth int) {
		// base case: successful search
		if len(displaced) == 0 {
			// score it
			scored := data.Score(working.Placements)

			// if we have a new best, clone the schedule and keep it
			if scored.Badness < working.Badness && scored.Badness < best.Badness {
				best = scored.Clone()
				//log.Printf("found a %d-swap improvement with score %d", depth, scored.Badness)
			}

			// continue swapping if there is still some depth left
			if maxDepth > depth {
				for _, placement := range working.Placements[placementIndex+1:] {
					displaced = append(displaced, placement)
					removeFromMatrix(placement)

					search(depth + 1)

					displaced = displaced[:len(displaced)-1]
					addToMatrix(placement)
				}
			}

			return
		}

		// base case: failed search
		if depth > maxDepth || len(displaced) > maxDepth-depth {
			return
		}

		// take one placement from the displaced list
		oldPlacement := displaced[len(displaced)-1]
		course := oldPlacement.Course
		displaced = displaced[:len(displaced)-1]

		// try every possible placement for it, adding to the displaced list as needed
		section := courseToSection[course]
		for r, times := range section.RoomTimes {
		timeLoop:
			for t, badness := range times {
				// cannot move it here if it is not allowed here
				if badness < 0 {
					continue
				}

				// cannot move it hereif it is not actually a move
				if r == oldPlacement.Room && t == oldPlacement.Time {
					continue
				}

				// which sections are in the way?
				var inTheWay []Placement
				slots := course.SlotsNeeded(data.Times[t])
				for si := 0; si < slots; si++ {
					target := working.RoomTimes[r][t+si].Course
					if target != nil {
						if len(inTheWay) == 0 || target != inTheWay[len(inTheWay)-1].Course {
							// cannot displace a course that we already moved
							for _, elt := range replaced {
								if target == elt {
									continue timeLoop
								}
							}
							index := courseToPlacementIndex[target]
							inTheWay = append(inTheWay, working.Placements[index])
						}
					}
				}

				newPlacement := Placement{
					Course: course,
					Room:   r,
					Time:   t,
				}

				// remove the in-the-way courses and push them to the displaced list
				for _, p := range inTheWay {
					displaced = append(displaced, p)
					removeFromMatrix(p)
				}

				// place the course here
				working.Placements[courseToPlacementIndex[course]] = newPlacement
				replaced = append(replaced, course)
				addToMatrix(newPlacement)

				// continue the search
				search(depth + 1)

				// undo the new placement
				removeFromMatrix(newPlacement)
				replaced = replaced[:len(replaced)-1]
				working.Placements[courseToPlacementIndex[course]] = oldPlacement

				// restore the in-the-way courses
				for _, p := range inTheWay {
					displaced = displaced[:len(displaced)-1]
					addToMatrix(p)
				}
			}
		}

		// move this course back to the displaced list
		displaced = append(displaced, oldPlacement)
	}

	// displace each section, then start a search for a new place to put it
	placement := working.Placements[placementIndex]
	displaced = append(displaced, placement)
	removeFromMatrix(placement)

	search(0)

	displaced = displaced[:len(displaced)-1]
	addToMatrix(placement)

	if len(displaced) != 0 {
		panic("swap search call did not clean up displaced list to empty")
	}

	return best
}
