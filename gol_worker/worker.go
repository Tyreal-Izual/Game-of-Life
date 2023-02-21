package main

import (
	"flag"
	"net"
	"net/rpc"
	"os"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type GOLWorker struct{}

var workerState stubs.WorkerState

func countAliveInOneRow(row []uint8, j int) int {
	count := 0
	for x := -1; x <= 1; x++ {
		if row[(j+len(row)+x)%len(row)] != 0 {
			count += 1
		}
	}
	return count
}

func countNeighbour(i int, j int, world [][]uint8) int {
	height := len(world)
	count := 0
	//alive cells in middle row
	count += countAliveInOneRow(world[i], j)
	//alive cells in the row above
	if i == 0 { //if it is the first row
		count += countAliveInOneRow(workerState.RowAbove, j)
	} else {
		count += countAliveInOneRow(world[i-1], j)
	}
	//alive cells in the row below
	if i == height-1 {
		count += countAliveInOneRow(workerState.RowBelow, j)
	} else {
		count += countAliveInOneRow(world[i+1], j)
	}
	//exclude itself
	if world[i][j] != 0 {
		count -= 1
	}
	return count
}

func worldAfterOneTurn() {
	//make newWorld to record the state after one turn
	height := workerState.Height
	width := workerState.Width
	world := workerState.WorldSlice
	newWorld := make([][]uint8, height, width)
	for i := 0; i < height; i++ {
		newWorld[i] = make([]uint8, width)
		copy(newWorld[i], world[i])
	}
	for h := 0; h < height; h++ {
		for w := 0; w < width; w++ {
			n := countNeighbour(h, w, world) //count alive neighbours of the previous world
			if (n < 2 || n > 3) && world[h][w] != 0 {
				newWorld[h][w] = 0
				workerState.FlippedCells = append(workerState.FlippedCells, util.Cell{X: w, Y: workerState.StartY + h})
			} else if n == 3 && world[h][w] == 0 {
				newWorld[h][w] = 0xFF
				workerState.FlippedCells = append(workerState.FlippedCells, util.Cell{X: w, Y: workerState.StartY + h})
			} else if world[h][w] == 0xFF {
				newWorld[h][w] = 0xFF
			} else {
				newWorld[h][w] = 0
			}
		}
	}
	workerState.WorldSlice = newWorld

}

func computeAliveCell() []util.Cell {
	var aliveCells []util.Cell
	for y, vy := range workerState.WorldSlice {
		for x, vx := range vy {
			if vx == 0xFF {
				c := util.Cell{X: x, Y: workerState.StartY + y}
				aliveCells = append(aliveCells, c)
			}
		}
	}
	return aliveCells
}

func (w *GOLWorker) WorkerSetUp(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	//world stored locally, only send back when required from broker
	workerState = stubs.WorkerState{
		WorldSlice:  req.WorldSlice,
		Height:      len(req.WorldSlice),
		Width:       len(req.WorldSlice[0]),
		RowAbove:    nil,
		RowBelow:    nil,
		CurrentTurn: 1,
		StartY:      req.StartY,
	}
	return
}

// HaloAfterOneTurn Calculates the world after one turn and stored locally, only give back halo region
func (w *GOLWorker) HaloAfterOneTurn(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	workerState.MutexLock.Lock()
	workerState.RowAbove = req.RowAbove
	workerState.RowBelow = req.RowBelow
	worldAfterOneTurn()
	resp.TopRow = workerState.WorldSlice[0]
	resp.BotRow = workerState.WorldSlice[workerState.Height-1]
	resp.FlippedCells = workerState.FlippedCells
	workerState.CurrentTurn += 1
	workerState.FlippedCells = nil
	workerState.MutexLock.Unlock()

	return
}

func (w *GOLWorker) WorkerAliveCellCount(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	workerState.MutexLock.Lock()
	resp.AliveCellCount = len(computeAliveCell())
	workerState.MutexLock.Unlock()
	return
}

func (w *GOLWorker) AliveCellList(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	workerState.MutexLock.Lock()
	resp.AliveCellList = computeAliveCell()
	workerState.MutexLock.Unlock()
	return
}

func (w *GOLWorker) GetWorld(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	workerState.MutexLock.Lock()
	resp.WorldSlice = workerState.WorldSlice
	workerState.MutexLock.Unlock()
	return
}

func (w *GOLWorker) FlippedCell(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	workerState.MutexLock.Lock()
	resp.FlippedCells = workerState.FlippedCells
	workerState.MutexLock.Unlock()
	return
}

func (w *GOLWorker) ShutDown(req stubs.WorkerRequest, resp *stubs.WorkerResponse) (err error) {
	os.Exit(0)
	return
}

func main() {
	port := flag.String("port", ":8030", "IP:port string to connect to as server")
	flag.Parse()
	ln, _ := net.Listen("tcp", *port)
	defer ln.Close()
	rpc.Register(&GOLWorker{})
	rpc.Accept(ln)
}
