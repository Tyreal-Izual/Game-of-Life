package gol

import (
	"fmt"
	"os"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events    chan<- Event
	ioCommand chan<- ioCommand
	ioIdle    <-chan bool

	ioFilename   chan<- string
	ioOutput     chan<- uint8
	ioInput      <-chan uint8
	ioKeyPresses <-chan rune
}

func initialiseWorld(p Params, c distributorChannels) [][]uint8 {
	count := 0
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	world := make([][]uint8, p.ImageHeight)
	//initialising world
	for row := range world {
		world[row] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[row][x] = <-c.ioInput
			if world[row][x] != 0 {
				c.events <- CellFlipped{CompletedTurns: 0, Cell: util.Cell{X: x, Y: row}}
				count += 1
			}
		}
	}
	return world
}

func countNeighbour(topEdge []uint8, botEdge []uint8, world [][]uint8, neighbours [][]int) {
	height := len(world)
	width := len(topEdge)
	//handle the  row above and the row below
	for i := 0; i < width; i++ {
		if topEdge[i] != 0 {
			neighbours[0][(i+width-1)%width] += 1
			neighbours[0][i] += 1
			neighbours[0][(i+width+1)%width] += 1
		}
		if botEdge[i] != 0 {
			neighbours[height-1][(i+width-1)%width] += 1
			neighbours[height-1][i] += 1
			neighbours[height-1][(i+width+1)%width] += 1
		}
	}
	//handle middle rows
	for y, row := range world {
		for x, cell := range row {
			if cell != 0 {
				//only one row
				if y == 0 && y == height-1 {
					for i := -1; i <= 1; i++ {
						neighbours[y][(x+width+i)%width] += 1
					}
				} else if y == 0 {
					for j := 0; j <= 1; j++ {
						for i := -1; i <= 1; i++ {
							neighbours[(y+height+j)%height][(x+width+i)%width] += 1
						}
					}
				} else if y == height-1 {
					for j := -1; j <= 0; j++ {
						for i := -1; i <= 1; i++ {
							neighbours[(y+height+j)%height][(x+width+i)%width] += 1
						}
					}
				} else {
					//add one to 8 cells around it
					for j := -1; j <= 1; j++ {
						for i := -1; i <= 1; i++ {
							neighbours[(y+height+j)%height][(x+width+i)%width] += 1
						}
					}
				}
				//exclude itself
				neighbours[y][x] -= 1
			}
		}
	}
}

func worldAfterOneTurn(width int, pieceOfWorld [][]uint8, topEdge []uint8, botEdge []uint8, startY int, c distributorChannels, turn int) [][]uint8 {
	//make newWorld to record the state after one turn
	newWorld := make([][]uint8, len(pieceOfWorld), width)
	neighbourCounts := make([][]int, len(pieceOfWorld), width)
	for i := 0; i < len(pieceOfWorld); i++ {
		newWorld[i] = make([]uint8, width)
		neighbourCounts[i] = make([]int, width)
		copy(newWorld[i], pieceOfWorld[i])
	}
	countNeighbour(topEdge, botEdge, pieceOfWorld, neighbourCounts) //count alive neighbours of the previous pieceOfWorld 			//index for newWorld created
	for h := 0; h < len(pieceOfWorld); h++ {
		for w := 0; w < width; w++ {
			if (neighbourCounts[h][w] < 2 || neighbourCounts[h][w] > 3) && pieceOfWorld[h][w] != 0 {
				newWorld[h][w] = 0
				//report the flip of the cell
				//startY + h making sure its reporting global location
				c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: w, Y: startY + h}}

			} else if neighbourCounts[h][w] == 3 && pieceOfWorld[h][w] == 0 {
				newWorld[h][w] = 0xFF
				//report the flip of the cell
				c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: w, Y: startY + h}}

			} else if pieceOfWorld[h][w] == 0xFF {
				newWorld[h][w] = 0xFF
			} else {
				newWorld[h][w] = 0
			}
		}
	}
	return newWorld
}

func worker(width int, pieceOfWorld [][]uint8, topEdge []uint8, botEdge []uint8, startY int, outChain chan<- [][]uint8, c distributorChannels, turn int) {
	outChain <- worldAfterOneTurn(width, pieceOfWorld, topEdge, botEdge, startY, c, turn)
}

func computeAliveCell(world [][]uint8) []util.Cell {
	var aliveCells []util.Cell
	for y, vy := range world {
		for x, vx := range vy {
			if vx == 0xFF {
				c := util.Cell{X: x, Y: y}
				aliveCells = append(aliveCells, c)
			}
		}
	}
	return aliveCells
}

// send the current state to the IO channel for output a PGM output file.
func currentState(p Params, world [][]byte, currentTurn int, c distributorChannels) {
	c.ioCommand <- ioOutput
	c.ioFilename <- fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, currentTurn)
	for _, y := range world {
		for _, x := range y {
			c.ioOutput <- x
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	//Create a 2D slice to store the world.
	world := initialiseWorld(p, c)
	turn := 0
	tickerChan := time.NewTicker(2 * time.Second)
	//Execute all turns of the Game of Life.
	var newWorld [][]uint8 //a world that's keep been updated
	var key rune
	var startY, endY, extraWorkLeft, thread int
	var distributedWorld [][]uint8
	var topEdge, botEdge []uint8
	var outChainForWorker chan [][]uint8
	//making out channels for workers to pass their output
	var outChannels []chan [][]uint8 //list of channels potentially contains the output from each worker
	for i := 0; i < p.Threads; i++ {
		outChan := make(chan [][]uint8)
		outChannels = append(outChannels, outChan)
	}

	for turn < p.Turns {
		select {
		case <-tickerChan.C:
			c.events <- AliveCellsCount{CompletedTurns: turn, CellsCount: len(computeAliveCell(world))}

		case key = <-c.ioKeyPresses:
			switch key {
			case 'p':
				fmt.Println("Current turn:", turn)
				for {
					if <-c.ioKeyPresses == 'p' {
						fmt.Println("Continuing")
						break
					}
				}
			case 'q':
				currentState(p, world, turn, c)
				os.Exit(0)
			case 's':
				currentState(p, world, turn, c)
			}
		default:
			startY = 0
			extraWorkLeft = p.ImageHeight % p.Threads //if work cannot be split equally, keep track of number of extra work left and assign to worker
			//Assign works to worker threads
			for thread = 0; thread < p.Threads; thread++ {
				endY = startY + (p.ImageHeight / p.Threads) - 1 //end = start + amount it suppose to do, -1 for start from 0
				if extraWorkLeft > 0 {
					endY += 1 //assign extra work to this worker
					extraWorkLeft--
				}
				outChainForWorker = outChannels[thread]
				//these are all passed by reference
				distributedWorld = world[startY : endY+1]               //only give the slice of the world that assigned to this worker
				topEdge = world[(startY+p.ImageHeight-1)%p.ImageHeight] //give the row above its piece of world
				botEdge = world[(endY+1+p.ImageHeight)%p.ImageHeight]   //give the row below its piece of world

				go worker(p.ImageWidth, distributedWorld, topEdge, botEdge, startY, outChainForWorker, c, turn)
				startY = endY + 1 //prepare for next worker
			}
			for thread = 0; thread < p.Threads; thread++ { //combining pieces of result to a new world
				newWorld = append(newWorld, <-outChannels[thread]...)
			}
			turn += 1
			c.events <- TurnComplete{CompletedTurns: turn} //Report the new state using Event.

			world = newWorld //make newWorld the current world
			newWorld = nil   //clean up newWorld for next turn

		}
	}
	tickerChan.Stop()
	//Report the final state using FinalTurnCompleteEvent.
	c.events <- FinalTurnComplete{CompletedTurns: turn, Alive: computeAliveCell(world)}

	//output PGM file
	currentState(p, world, p.Turns, c)
	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
