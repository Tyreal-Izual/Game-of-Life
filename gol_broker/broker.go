package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"sync"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type GOLBroker struct{}

var worldState stubs.WorldStates
var backUpWorldState stubs.WorldStates

func readIP() {
	var ipList []string
	count := 0
	file, err := os.Open("IP")
	if err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(file)
	defer file.Close()
	for scanner.Scan() {
		ipList = append(ipList, scanner.Text())
		count++
	}
	if len(ipList) < 1 {
		log.Fatal(errors.New("empty IP file"))
	}
	worldState.ConnectionList = ipList
	return
}

func connectToWorkers(ip string) *rpc.Client {
	//read ip of worker server from command line
	client, err := rpc.Dial("tcp", ip)
	if err != nil {
		log.Fatal(err)
	}
	return client
}

// workerInitialisation set up worldState.WorkerList with corresponding WorkerRecord
func workerInitialisation(height int) {
	startY := 0
	extraWorkLeft := height % worldState.Threads //if work cannot be split equally, keep track of number of extra work left and assign to worker
	for thread := 0; thread < worldState.Threads; thread++ {
		endY := startY + (height / worldState.Threads) - 1 //end = start + amount it suppose to do, -1 for start from 0
		if extraWorkLeft > 0 {
			endY += 1 //assign extra work to this worker
			extraWorkLeft--
		}
		//initialise worker and its responsible part of world
		worker := stubs.WorkerRecord{
			WorkerNumber: thread,
			Connection:   connectToWorkers(worldState.ConnectionList[thread]),
			WorldSlice:   worldState.CurrentWorld[startY : endY+1], //only give the slice of the world that assigned to this worker
			StartY:       startY,
			EndY:         endY,
			TopRow:       worldState.CurrentWorld[startY], //give the row above its piece of world
			BotRow:       worldState.CurrentWorld[endY],   //give the row below its piece of world
		}
		worldState.WorkerList[thread] = worker
		request := stubs.WorkerRequest{
			WorldSlice: worker.WorldSlice,
			RowAbove:   worker.TopRow,
			RowBelow:   worker.BotRow,
			StartY:     startY,
		}
		response := new(stubs.WorkerResponse)
		worker.Connection.Call(stubs.WorkerSetUp, request, response)
		startY = endY + 1
	}
}

func distributeWork(workerNum int, worker stubs.WorkerRecord) {
	//Only give in its halo region
	request := stubs.WorkerRequest{
		CurrentTurn: worldState.CurrentTurn + 1,
		RowAbove:    worldState.WorkerList[(worldState.Threads+workerNum-1)%worldState.Threads].BotRow, //last row of the worker before it
		RowBelow:    worldState.WorkerList[(worldState.Threads+workerNum+1)%worldState.Threads].TopRow, //first row of the worker after it
	}
	response := new(stubs.WorkerResponse)
	worker.Connection.Call(stubs.HaloAfterOneTurn, request, response)
	worldState.WorkRequestSentChan <- true
	//make sure all request for current turn has been sent before change state for the next turn
	<-worldState.AllowChangeChan
	worldState.WorkerList[workerNum].TopRow = response.TopRow
	worldState.WorkerList[workerNum].BotRow = response.BotRow
	worldState.MutexLock.Lock()
	//for sdl display
	worldState.FlippedCell = append(worldState.FlippedCell, response.FlippedCells...)
	worldState.MutexLock.Unlock()
	//ready for next turn
	worldState.DistributedWorkDoneChan <- true
}

func gatherAliveCellCount() int {
	count := 0
	for _, worker := range worldState.WorkerList {
		request := stubs.WorkerRequest{}
		response := new(stubs.WorkerResponse)
		worker.Connection.Call(stubs.WorkerAliveCellCount, request, response)
		count += response.AliveCellCount
	}
	return count
}

func getAliveCellList() []util.Cell {
	var aliveCellList []util.Cell
	for _, worker := range worldState.WorkerList {
		request := stubs.WorkerRequest{}
		response := new(stubs.WorkerResponse)
		worker.Connection.Call(stubs.AliveCellList, request, response)
		aliveCellList = append(aliveCellList, response.AliveCellList...)
	}
	return aliveCellList
}

func gatherWorld() {
	var completeWorld [][]uint8
	for _, worker := range worldState.WorkerList {
		request := stubs.WorkerRequest{}
		response := new(stubs.WorkerResponse)
		worker.Connection.Call(stubs.GetWorld, request, response)
		completeWorld = append(completeWorld, response.WorldSlice...) //unpack slice and append
	}
	worldState.CurrentWorld = completeWorld
}

func reportFlippedCell() {
	distributor, err := rpc.Dial("tcp", "127.0.0.1:8010")
	if err != nil {
		log.Fatal(err)
	}
	request := stubs.DistributorRequest{
		FlippedCells: worldState.FlippedCell,
		Turn:         worldState.CurrentTurn,
	}
	response := stubs.DistributorResponse{}
	distributor.Call(stubs.SdlDisplay, request, response)
	worldState.FlippedCell = nil
}

func (b *GOLBroker) Broker(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	if req.ContinueWorld == false {
		//Initialisation
		worldState = stubs.WorldStates{
			CurrentTurn:             0,
			CurrentWorld:            req.World,
			TotalTurns:              req.Turn,
			MutexLock:               sync.Mutex{},
			PauseChan:               make(chan bool),
			Threads:                 req.Threads,
			DistributedWorkDoneChan: make(chan bool),
			WorkRequestSentChan:     make(chan bool),
			AllowChangeChan:         make(chan bool),
		}
		readIP() //record IP address of all workers and add to worldState.ConnectionList
		if len(worldState.ConnectionList) < worldState.Threads {
			fmt.Printf("Expecting %d threads but only reads %d IP address\n", worldState.Threads, len(worldState.ConnectionList))
			worldState.Threads = len(worldState.ConnectionList)
			fmt.Printf("Changed to %d threads, or put in more address in IP and restart\n", worldState.Threads)
		}
		//List to store information about every worker and give every worker their initial part of the world
		workers := make([]stubs.WorkerRecord, worldState.Threads)
		worldState.WorkerList = workers
		worldState.MutexLock.Lock()
		workerInitialisation(req.ImageHeight)
		worldState.MutexLock.Unlock()
	} else {
		//continue from last world, create new channels and lock to avoid possible fault
		if backUpWorldState.CurrentWorld == nil {
			return errors.New("no previous world found")
		}
		worldState = backUpWorldState
		worldState.MutexLock = sync.Mutex{}
		worldState.DistributedWorkDoneChan = make(chan bool)
		worldState.WorkRequestSentChan = make(chan bool)
		worldState.AllowChangeChan = make(chan bool)
	}
	//calculate for each turn
	for worldState.CurrentTurn < worldState.TotalTurns {
		select {
		//pausing the server if required
		case <-worldState.PauseChan:
			for {
				if <-worldState.PauseChan == false {
					break
				}
			}
		//compute current turn and update the world after each turn
		default:
			for i, worker := range worldState.WorkerList {
				go distributeWork(i, worker) //assign work to worker
			}
			//make sure all request for current turn has been changed before allowing to change state for next turn
			for i := 0; i < worldState.Threads; i++ {
				<-worldState.WorkRequestSentChan
			}
			for i := 0; i < worldState.Threads; i++ {
				worldState.AllowChangeChan <- true
			}

			//make sure every distributeWork goroutine have updated their workerRecord and ready for next turn
			for count := 0; count < worldState.Threads; count++ {
				<-worldState.DistributedWorkDoneChan
			}
			worldState.MutexLock.Lock()
			worldState.CurrentTurn += 1
			reportFlippedCell()
			worldState.MutexLock.Unlock()
		}
	}
	//report final state
	worldState.MutexLock.Lock()
	gatherWorld()
	resp.Turn = worldState.CurrentTurn
	resp.AliveCellList = getAliveCellList()
	resp.World = worldState.CurrentWorld
	return
}

func (b *GOLBroker) AliveCellCount(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	worldState.MutexLock.Lock()
	resp.Turn = worldState.CurrentTurn + 1       //respond current turn
	resp.AliveCellCount = gatherAliveCellCount() //respond no. of alive cell in current turn
	worldState.MutexLock.Unlock()
	return
}

func (b *GOLBroker) GetCurrentWorld(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	worldState.MutexLock.Lock()
	gatherWorld()
	resp.World = worldState.CurrentWorld
	resp.Turn = worldState.CurrentTurn
	worldState.MutexLock.Unlock()
	return
}

// Reset clean up and get ready for next clients to take over
func (b *GOLBroker) Reset(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	worldState.MutexLock.Lock()
	backUpWorldState = worldState //save the current world state
	worldState.CurrentTurn = 0
	worldState.CurrentWorld = nil
	worldState.TotalTurns = 0
	worldState.PauseChan = nil
	worldState.Threads = 1
	worldState.MutexLock.Unlock()
	return

}

// ShutServer send the last state of the world to client and shut down
func (b *GOLBroker) ShutServer(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	request := stubs.WorkerRequest{}
	response := new(stubs.WorkerResponse)
	for _, worker := range worldState.WorkerList {
		worker.Connection.Call(stubs.ShutWorker, request, response)
	}
	os.Exit(0)
	return
}

func (b *GOLBroker) PauseServer(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	resp.Turn = worldState.CurrentTurn
	worldState.PauseChan <- true
	return
}

func (b *GOLBroker) ResumeServer(req stubs.BrokerRequest, resp *stubs.BrokerResponse) (err error) {
	worldState.PauseChan <- false
	return
}

func main() {
	ln, _ := net.Listen("tcp", ":8060")
	defer ln.Close()
	rpc.Register(&GOLBroker{})
	rpc.Accept(ln)
}
