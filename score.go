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

func (data *InputData) Score(grid [][]Cell) Schedule {
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

	// check how many rooms the instructor is assigned to
	// check how spread out the instructor's schedule is

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
