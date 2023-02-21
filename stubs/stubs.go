package stubs

import (
	"net/rpc"
	"sync"
	"uk.ac.bris.cs/gameoflife/util"
)

var Broker = "GOLBroker.Broker"
var AliveCellsCount = "GOLBroker.AliveCellCount"
var GetCurrentWorld = "GOLBroker.GetCurrentWorld"
var ResetServer = "GOLBroker.Reset"
var ShutServer = "GOLBroker.ShutServer"
var PauseServer = "GOLBroker.PauseServer"
var ResumeServer = "GOLBroker.ResumeServer"

var WorkerSetUp = "GOLWorker.WorkerSetUp"
var HaloAfterOneTurn = "GOLWorker.HaloAfterOneTurn"
var WorkerAliveCellCount = "GOLWorker.WorkerAliveCellCount"
var AliveCellList = "GOLWorker.AliveCellList"
var GetWorld = "GOLWorker.GetWorld"
var FlippedCell = "GOLWorker.FlippedCell"
var ShutWorker = "GOLWorker.ShutDown"

var SdlDisplay = "DistributorGOL.SdlDisplay"

// WorldStates for broker to track the state of the complete world
type WorldStates struct {
	CurrentTurn             int
	CurrentWorld            [][]uint8
	TotalTurns              int
	MutexLock               sync.Mutex
	PauseChan               chan bool
	Threads                 int
	ConnectionList          []string
	WorkerList              []WorkerRecord
	DistributedWorkDoneChan chan bool //make sure every distributeWork goroutine are ready for next turn
	WorkRequestSentChan     chan bool
	AllowChangeChan         chan bool //make sure there is no state change before the all request for current turn has sent
	FlippedCell             []util.Cell
}

// WorkerState for worker to keep track of the world
type WorkerState struct {
	WorldSlice   [][]uint8 //update by itself
	Height       int
	Width        int
	RowAbove     []uint8 //update by the request sent by broker for every turn
	RowBelow     []uint8 //update by the request sent by broker for every turn
	MutexLock    sync.Mutex
	FlippedCells []util.Cell
	CurrentTurn  int
	StartY       int //for it to report correct alive cell
}

// WorkerRecord keep track of every worker
type WorkerRecord struct {
	WorkerNumber int
	Connection   *rpc.Client
	WorldSlice   [][]uint8
	StartY       int
	EndY         int
	TopRow       []uint8 //TopRow of itself, this will be enquired by its neighbouring worker
	BotRow       []uint8 //BotRow of itself, this will be enquired by its neighbouring worker
}

type BrokerRequest struct {
	ContinueWorld bool      //identify whether server should continue from last world
	World         [][]uint8 //2D slice to store the initial world
	Turn          int       //total number  of turns expected to be complete
	Threads       int
	ImageHeight   int
	ImageWidth    int
}

type BrokerResponse struct {
	World          [][]uint8
	AliveCellList  []util.Cell
	AliveCellCount int
	Turn           int
	FlippedCells   []util.Cell
}

type WorkerRequest struct {
	WorldSlice  [][]uint8
	RowAbove    []uint8 //halo region it required
	RowBelow    []uint8
	CurrentTurn int
	StartY      int
}

type WorkerResponse struct {
	WorldSlice     [][]uint8
	TopRow         []uint8
	BotRow         []uint8
	AliveCellCount int
	AliveCellList  []util.Cell
	FlippedCells   []util.Cell
}

type DistributorRequest struct {
	FlippedCells []util.Cell
	Turn         int
}

type DistributorResponse struct{}
