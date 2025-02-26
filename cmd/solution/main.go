package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
)

func main() {
	debug.SetGCPercent(-1)
	debug.SetMemoryLimit(math.MaxInt64)
	if os.Getenv("PROFILE") != "" {
		profFileName := os.Args[0] + ".prof"
		fmt.Fprintln(os.Stderr, "### Profiling enabled")
		pfile, err := os.Create(profFileName)
		if err != nil {
			panic(err)
		}
		defer pfile.Close()
		pprof.StartCPUProfile(pfile)
		defer func() {
			pprof.StopCPUProfile()
			fmt.Fprintln(os.Stderr, "### Profiling done, run:\ngo tool pprof", os.Args[0], profFileName)
		}()
	}

	inputFile := "measurements.txt"
	if len(os.Args) > 1 {
		inputFile = os.Args[1]
	}
	fmt.Fprintln(os.Stderr, "Reading records from", inputFile)

	// Add a few bytes of padding so we can read past end of file for some calculations
	fileMap, err := NewMmapFile(inputFile, 3)
	if err != nil {
		panic(err)
	}
	defer fileMap.Close()

	stats := processParallel(fileMap.Data)
	for item := range stats.Entries() {
		mMax := Decimal1_16ToFloat(item.Max)
		mMin := Decimal1_16ToFloat(item.Min)
		mAvg := Decimal1_64ToFloat(item.Sum) / float64(item.Count)
		fmt.Printf("%s;%0.1f;%0.1f;%0.1f\n", item.Name, mMax, mMin, mAvg)
	}
}

func processParallel(data []byte) *ProcessedResults {
	lookup := PrepareDecimal1Lookup()
	partitions := partitionData(data, runtime.NumCPU())
	resultsCh := make(chan *ProcessedResults)
	for _, partition := range partitions {
		go process(partition, resultsCh, &lookup)
	}
	stats, err := Alloc[ProcessedResults](ProcessedResultsSize)
	if err != nil {
		panic(err)
	}
	for range partitions {
		stats.MergeFrom(<-resultsCh)
	}
	return stats
}

func partitionData(data []byte, numPartitions int) [][]byte {
	partitions := make([][]byte, numPartitions)
	partitionSize := len(data) / numPartitions
	prevEnd := 0
	for i := range numPartitions {
		start := prevEnd
		end := max(start, partitionSize*(i+1))
		if i == numPartitions-1 {
			end = len(data)
		} else {
			for data[end-1] != byte('\n') && end < len(data) {
				end += 1
			}
		}
		prevEnd = end
		partitions[i] = data[start:end]
	}
	return partitions
}

func process(data []byte, resultCh chan *ProcessedResults, lookup *[65536]Decimal1_16) {
	results, err := Alloc[ProcessedResults](ProcessedResultsSize)
	if err != nil {
		fmt.Fprint(os.Stderr, "Could not allocate huge pages. Try:\nsudo sysctl -w vm.nr_hugepages=512\n")
		panic(err)
	}
	IterInto(data, results, lookup)
	resultCh <- results
}
