package gol

import (
	"fmt"
	"log"
	"net"
	"net/rpc"
	"os"
	"time"
	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

var disChan distributorChannels

type distributorChannels struct {
	events       chan<- Event
	ioCommand    chan<- ioCommand
	ioIdle       <-chan bool
	ioFilename   chan<- string
	ioOutput     chan<- uint8
	ioInput      <-chan uint8
	ioKeyPresses <-chan rune
}

func initialiseWorld(p Params, c distributorChannels) [][]uint8 {
	disChan = c
	count := 0
	c.ioCommand <- ioInput
	c.ioFilename <- fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	world := make([][]uint8, p.ImageHeight)
	//initialising world
	for row := range world {
		world[row] = make([]byte, p.ImageWidth)
		for x := 0; x < p.ImageWidth; x++ {
			world[row][x] = <-c.ioInput
			//Initialise sdl
			if world[row][x] != 0 {
				c.events <- CellFlipped{CompletedTurns: 0, Cell: util.Cell{X: x, Y: row}}
				count += 1
			}
		}
	}
	return world
}

func connect() *rpc.Client {
	//read ip of worker server from command line
	client, err := rpc.Dial("tcp", "127.0.0.1:8060")
	if err != nil {
		log.Fatal(err)
	}
	return client
}

// loopingLogic take care of alive cell count every 2 sec and key press during turns
func loopingLogic(client *rpc.Client, tickerChan time.Ticker, p Params, c distributorChannels, stopLoop chan bool) {
	var done bool
	var key rune
	for !done {
		select {
		case <-tickerChan.C:
			response := new(stubs.BrokerResponse)
			err := client.Call(stubs.AliveCellsCount, new(stubs.BrokerRequest), response)
			if err != nil {
				log.Fatal(err)
			}
			c.events <- AliveCellsCount{
				CompletedTurns: response.Turn,
				CellsCount:     response.AliveCellCount,
			}
		case key = <-c.ioKeyPresses:
			request := stubs.BrokerRequest{}
			response := new(stubs.BrokerResponse)
			switch key {
			case 's':
				client.Call(stubs.GetCurrentWorld, request, response)
				currentState(p, response.World, response.Turn, c)
			case 'q': //close controller
				client.Call(stubs.ResetServer, request, response)
				//os.Exit(0)
			case 'k':
				client.Call(stubs.GetCurrentWorld, request, response)
				currentState(p, response.World, response.Turn, c)
				client.Call(stubs.ShutServer, request, response)
				os.Exit(0)
			case 'p':
				client.Call(stubs.PauseServer, request, response)
				fmt.Println("Current turn:", response.Turn)
				for {
					if <-c.ioKeyPresses == 'p' {
						fmt.Println("Continuing")
						client.Call(stubs.ResumeServer, request, response)
						break
					}
				}
			}
		case done = <-stopLoop:
		}

	}
}

// currentState send the current state to the IO channel for output a PGM output file.
func currentState(p Params, world [][]byte, currentTurn int, c distributorChannels) {
	c.ioCommand <- ioOutput
	c.ioFilename <- fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, currentTurn)
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	//Listen for update from broker for sdl display
	rpc.Register(&DistributorGOL{})
	ln, err := net.Listen("tcp", ":8010")
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	go rpc.Accept(ln)
	//initialisation
	client := connect()
	defer client.Close()
	world := initialiseWorld(p, c)
	tickerChan := time.NewTicker(2 * time.Second)
	stopLoopingChan := make(chan bool)
	go loopingLogic(client, *tickerChan, p, c, stopLoopingChan)
	request := stubs.BrokerRequest{
		ContinueWorld: p.Continue,
		World:         world,
		Turn:          p.Turns,
		ImageHeight:   p.ImageHeight,
		ImageWidth:    p.ImageWidth,
		Threads:       p.Threads,
	}
	response := new(stubs.BrokerResponse)
	//send request to broker to start calculation
	client.Call(stubs.Broker, request, response)
	//send the final state returned from broker to events channel
	c.events <- FinalTurnComplete{
		CompletedTurns: response.Turn,
		Alive:          response.AliveCellList,
	}

	//output PGM file of the final state
	currentState(p, response.World, p.Turns, c)
	stopLoopingChan <- true
	tickerChan.Stop()

	//Execute all turns of the Game of Life.
	//making out channels for workers to pass their output
	//Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
	ln.Close()

}

type DistributorGOL struct{}

func (d *DistributorGOL) SdlDisplay(req stubs.DistributorRequest, resp *stubs.DistributorResponse) (err error) {
	for _, cell := range req.FlippedCells {
		disChan.events <- CellFlipped{Cell: cell, CompletedTurns: req.Turn}
	}
	disChan.events <- TurnComplete{CompletedTurns: req.Turn}
	return
}
