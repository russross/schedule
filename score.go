package main

import (
	"fmt"
	"sort"
)

// A Schedule is a two-dimensional view of the placed sections,
// ready to be scored and displayed.
type Schedule struct {
	RoomTimes [][]Cell
	Problems  []string
	Badness   int
}

type Problem struct {
	Message string
	Badness int
}

func (s *Schedule) AddBadness(badness int) {
	if badness >= 0 && badness < 100 {
		s.Badness += badness
	} else {
		s.Badness += Impossible
	}
}

const Impossible int = 1000000

func (data *InputData) Score(placements []Placement) Schedule {
	grid := data.MakeGrid(placements)
	schedule := Schedule{RoomTimes: grid}
	var problems []Problem

	// check each time slot
	for t := range data.Times {
		// consider each course in this time slot
		for roomA := 0; roomA < len(data.Rooms); roomA++ {
			courseA := grid[roomA][t].Course
			if courseA == nil {
				continue
			}

			// is this a bad time for this instructor?
			if badness := courseA.Instructor.Times[t]; badness > 0 && badness < 100 {
				msg := fmt.Sprintf("instructor time preference: %s has %s scheduled at %s (badness %d)",
					courseA.Instructor.Name, courseA.Name, data.Times[t].Name, badness)
				problems = append(problems, Problem{Message: msg, Badness: badness})
			} else if badness < 0 || badness >= 100 {
				msg := fmt.Sprintf("instructor not available: %s has %s scheduled at %s (badness %d)",
					courseA.Instructor.Name, courseA.Name, data.Times[t].Name, Impossible)
				problems = append(problems, Problem{Message: msg, Badness: Impossible})
			}

			// is this a bad time for this course?
			if len(courseA.Times) > 0 {
				if badness := courseA.Times[t]; badness != 0 {
					if badness < 0 || badness >= 100 {
						badness = Impossible
					}
					msg := fmt.Sprintf("course time preference: %s should not be scheduled at %s (badness %d)",
						courseA.Name, data.Times[t].Name, badness)
					problems = append(problems, Problem{Message: msg, Badness: badness})
				}
			}

			// is this a bad room for this course?
			if badness := courseA.Rooms[roomA]; badness != 0 {
				if badness < 0 || badness >= 100 {
					badness = Impossible
				}
				msg := fmt.Sprintf("course room preference: %s should not be scheduled in %s (badness %d)",
					courseA.Name, data.Rooms[roomA].Name, badness)
				problems = append(problems, Problem{Message: msg, Badness: badness})
			}

			// compare pairs of courses in different rooms at the same time
			for roomB := roomA + 1; roomB < len(data.Rooms); roomB++ {
				courseB := grid[roomB][t].Course
				if courseB == nil {
					continue
				}

				// are these taught by the same instructor?
				// (note: we will never generate a schedule like this,
				// but a user might propose one)
				if courseA.Instructor == courseB.Instructor {
					if !grid[roomA][t].IsSpillover || !grid[roomB][t].IsSpillover {
						courses := []string{courseA.Name, courseB.Name}
						sort.Strings(courses)
						msg := fmt.Sprintf("instructor double booked: %s has courses %s and %s at %s (badness %d)",
							courseA.Instructor.Name, courses[0], courses[1], data.Times[t].Name, Impossible)
						problems = append(problems, Problem{Message: msg, Badness: Impossible})
					}
				}

				// are these two courses in conflict?
				if badness, present := courseA.Conflicts[courseB]; present {
					if !grid[roomA][t].IsSpillover || !grid[roomB][t].IsSpillover {
						if badness < 0 {
							badness = Impossible
						}
						courses := []string{courseA.Name, courseB.Name}
						sort.Strings(courses)
						msg := fmt.Sprintf("curriculum conflict: %s and %s both meet at %s (badness %d)",
							courses[0], courses[1], data.Times[t].Name, badness)
						problems = append(problems, Problem{Message: msg, Badness: badness})
					}
				}
			}
		}
	}

	// find what count as days (multiple time slots with the same prefix)
	timesPerDay := make(map[string]int)
	for _, time := range data.Times {
		if prefix := time.Prefix(); prefix != "" {
			timesPerDay[prefix]++
		}
	}

	// check how many rooms the instructor is assigned to
	// check how spread out the instructor's schedule is
	// check the split of an instructor's classes across days
	instructorToPlacements := make(map[*Instructor][]Placement)
	for _, placement := range placements {
		lst := instructorToPlacements[placement.Course.Instructor]
		instructorToPlacements[placement.Course.Instructor] = append(lst, placement)
	}

	// check each instructor's schedule for niceness
	for instructor, list := range instructorToPlacements {
		sort.Slice(list, func(a, b int) bool {
			return list[a].Time < list[b].Time
		})

		// gather info about how many classes are in each room and on each day
		inRoom := make(map[int]int)
		onDay := make(map[string][]Placement)
		for _, elt := range list {
			inRoom[elt.Room]++
			if prefix := data.Times[elt.Time].Prefix(); timesPerDay[prefix] > 1 {
				onDay[prefix] = append(onDay[prefix], elt)
			}
		}

		// penalize instructors with courses in too many rooms
		if extra := len(inRoom) - instructor.MinRooms; extra > 0 {
			badness := extra * extra
			msg := fmt.Sprintf("instructor convenience: %s is spread across more rooms than necessary (badness %d)",
				instructor.Name, badness)
			problems = append(problems, Problem{Message: msg, Badness: badness})
		}

		// penalize workloads that are unevenly split across days
		if len(onDay) > 1 {
			max, min := -1, -1
			i := 0
			for _, classes := range onDay {
				count := len(classes)
				if i == 0 || count > max {
					max = count
				}
				if i == 0 || count < min {
					min = count
				}
				i++
			}

			// add a penalty if there is more than one class difference between
			// the most and fewest on a day
			if gap := max - min; gap > 1 {
				badness := gap * gap
				msg := fmt.Sprintf("instructor convenience: %s has more classes on some days than others (badess %d)",
					instructor.Name, badness)
				problems = append(problems, Problem{Message: msg, Badness: badness})
			}
		}

		// try to honor instructor preference for number of days teaching
		if instructor.Days > 0 && len(onDay) != instructor.Days {
			gap := instructor.Days - len(onDay)
			if gap < 0 {
				gap = -gap
			}
			badness := 10 * gap
			wanted := "s"
			if instructor.Days == 1 {
				wanted = ""
			}
			got := "s"
			if len(onDay) == 1 {
				got = ""
			}
			msg := fmt.Sprintf("instructor preference: %s has classes on %d day%s but wanted them on %d day%s (badness %d)",
				instructor.Name, len(onDay), got, instructor.Days, wanted, badness)
			problems = append(problems, Problem{Message: msg, Badness: badness})
		}

		if len(instructor.Courses) > 1 {
			badness := 0

			// penalize schedules that are too spread out or too clustered on a given day
			for _, classes := range onDay {
				// if there are an odd number of classes this day, it's okay to have a lone class
				singletonOkay := len(classes)&1 == 1
				i := 0
				for i < len(classes) {
					// find the beginning of the next cluster of classes (if any)
					slotsNeeded := classes[i].Course.SlotsNeeded(data.Times[classes[i].Time])
					var next int
					for next = i + 1; next < len(classes); next++ {
						if classes[next].Time-classes[next-1].Time > slotsNeeded {
							size := classes[next].Time - classes[next-1].Time - slotsNeeded

							// is this gap too long?
							if size > 1 {
								// 2 => 4, 3 => 9, 4 => 16
								badness += size * size
							}

							break
						}
						slotsNeeded = classes[next].Course.SlotsNeeded(data.Times[classes[next].Time])
					}

					clusterSize := next - i
					i = next

					// was this cluster of classes too long or too short?
					if clusterSize == 1 && singletonOkay {
						// this is the odd class on this day
						singletonOkay = false
					} else {
						// clusters of size two are perfect, anything else gets a penalty
						mismatch := clusterSize - 2
						if mismatch < 0 {
							mismatch = -mismatch
						}
						if mismatch != 0 {
							// 1 => 2, 3 => 2, 4 => 6, 5 => 12
							badness += mismatch * (mismatch + 1)
						}
					}
				}
			}

			if badness > 0 {
				msg := fmt.Sprintf("instructor convenience: %s has classes that are poorly spread out (badness %d)",
					instructor.Name, badness)
				problems = append(problems, Problem{Message: msg, Badness: badness})
			}
		}
	}

	sort.Slice(problems, func(a, b int) bool {
		if problems[a].Badness != problems[b].Badness {
			return problems[a].Badness > problems[b].Badness
		}
		return problems[a].Message < problems[b].Message
	})
	for _, problem := range problems {
		schedule.Problems = append(schedule.Problems, problem.Message)
		schedule.AddBadness(problem.Badness)
	}
	return schedule
}
