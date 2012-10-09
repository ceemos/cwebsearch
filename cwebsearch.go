// Copyright 2012 Marcel Schneider
// heavily based on csearch:
	// Copyright 2011 The Go Authors.  All rights reserved.
	// Use of this source code is governed by a BSD-style
	// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"io"
	"time"
	"strconv"
	"strings"
	"bytes"
	"io/ioutil"
	
	"code.google.com/p/codesearch/index"
	"code.google.com/p/codesearch/regexp"
	
	"net/http"
)

// Web serving code
func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/cws.js" {
		w.Header().Set("Content-Type", "text/javascript")
		writeFile(w, "cws.js")
		return
	}
	w.Header().Set("Content-Type", "text/html")
	q := r.FormValue("q")
	if q == "" {
		writeFile(w, "form.html")
		return
	}
	limit, err := strconv.Atoi(r.FormValue("l"))
	if err != nil {
		limit = 10
	}
	timelimit, err := strconv.Atoi(r.FormValue("t"))
	if err != nil {
		timelimit = 1000
	}
	fflag := ""
	readFflag := false
	iflag := false
	args := strings.Split(q, " ")
	res := make([]string, 0, len(args))
	for _, v := range args {
		switch {
			case readFflag:
				fflag = v
				readFflag = false
			case v == "-f":
				readFflag = true
			case v == "-i":
				iflag = true
			default:
				res = append(res, v)
		}
	}
	fmt.Println("Starting query", res)
	lines := query(res, fflag, iflag, w, limit, time.Duration(timelimit) * time.Millisecond)
	if lines == 0 {
		fmt.Fprintln(w, "...:0: No Results Found.")
	}
}

func writeFile(w http.ResponseWriter, file string) {
	f, err := os.Open(file)
	if err != nil {
		fmt.Fprintf(w, "FATAL: Cant open %s: %s", file, err)
		return
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		fmt.Fprintf(w, "FATAL: Cant read form %s: %s",file , err)
		return
	}
	w.Write(data)
}


var usageMessage = `usage: cwebsearch 
`

func usage() {
	fmt.Fprintf(os.Stderr, usageMessage)
	os.Exit(2)
}

var (
	verboseFlag = flag.Bool("verbose", false, "print extra information")
	bruteFlag   = flag.Bool("brute", false, "brute force - search all files in index")
	cpuProfile  = flag.String("cpuprofile", "", "write cpu profile to this file")

)

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()

	if len(args) != 0 {
		usage()
	}

	http.HandleFunc("/", handleQuery)
	http.ListenAndServe("localhost:4000", nil)
}

func query(patterns []string, fFlag string, iFlag bool, out io.Writer, limit int, timelimit time.Duration) (lines int) {
	
	var fre *regexp.Regexp
	var err error
	if fFlag != "" {
		fre, err = regexp.Compile(fFlag)
		if err != nil {
			return
		}
	}
	outchan := make(chan string) // all output ist collected here.
	matchchan := make(chan bool) // grep's tell whether thy found sth.
	stopchan := make(chan bool) // grep's listen here to be stopped
	timeout := make(chan bool) // delivers a timeout for this function
	go func() {
        time.Sleep(timelimit)
        timeout <- true
    }()
	
	g := make([]*Grep, 0, len(patterns))
	for _, v := range patterns {
		pat := "(?m)" + v
		if iFlag {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			continue
		}
		log.Printf("Grepping for %s\n", re)
		g = append(g, &Grep{
			Regexp: re,
			Stdout: outchan,
			Matched: matchchan,
			Stop: stopchan,
			Stderr: os.Stderr,
		})
	}
	if len(g) == 0 {
		return
	}
	
	q := index.RegexpQuery(g[0].Regexp.Syntax)
	for _, v := range g[1:] {
		q = q.And(index.RegexpQuery(v.Regexp.Syntax))
	}
	if *verboseFlag {
		log.Printf("query: %s\n", q)
	}

	ix := index.Open(index.File())
	ix.Verbose = *verboseFlag
	var post []uint32
	if *bruteFlag {
		post = ix.PostingQuery(&index.Query{Op: index.QAll})
	} else {
		post = ix.PostingQuery(q)
	}
	if *verboseFlag {
		log.Printf("post query identified %d possible files\n", len(post))
	}

	if fre != nil {
		fnames := make([]uint32, 0, len(post))

		for _, fileid := range post {
			name := ix.Name(fileid)
			if fre.MatchString(name, true, true) < 0 {
				continue
			}
			fnames = append(fnames, fileid)
		}

		if *verboseFlag {
			log.Printf("filename regexp matched %d files\n", len(fnames))
		}
		post = fnames
	}

	output := make([]string, 0, 10)
	lines = 0
	timeoutFlag := false
	for _, fileid := range post {
		output = output[:0]
		name := ix.Name(fileid)
		
		for _, grep := range g {
			go grep.File(name)
		}
		runningcount := len(g)
		
		// Counting is critical here. Read once from matchchan and write once 
		// to stopchan for ech grep - or everything will deadlock.
		matched := true
		for runningcount > 0 {
			select {
				case s := <- outchan:
					output = append(output, s)
				case match := <- matchchan:
					runningcount--
					if !match {
						matched = false
						runningcount = 0
					}
				case <- timeout:
					runningcount = 0
					timeoutFlag = true
			}
			
		}
		//log.Println("Stopping all greps")
		stopcount := len(g)
		for stopcount > 0 {
			select {
				case stopchan <- true:
					stopcount--
				case <- outchan:
				case <- matchchan:
			}
		}
		//log.Println("All greps stopped")
		if matched {
			if *verboseFlag {
				log.Printf("writing %d lines of output from %s\n", len(output), name)
			}
			for _, s := range output {
				fmt.Fprint(out, s)
				lines++
				limit--
				if limit == 0 {
					fmt.Fprint(out, "... :0: Even More.\n")
					return
				}
			}
		}
		if timeoutFlag {
			fmt.Fprintf(out, "... :0: Timeout: %dms.\n", timelimit / time.Millisecond)
			break;
		}
			
	}
	return
}

// based on regexp.Grep, modded for channel communication
type Grep struct {
	Regexp *regexp.Regexp   // regexp to search for
	
	Stderr io.Writer // error target
	
	Stdout chan string // output target
	Matched chan bool // report if a match was found
	Stop chan bool // when sth. enters here, search is stopprd

	Match bool

	buf []byte
}

func (g *Grep) File(name string) {
	f, err := os.Open(name)
	if err != nil {
		fmt.Fprintf(g.Stderr, "%s\n", err)
		<- g.Stop
		return
	}
	defer f.Close()
	g.Reader(f, name)
}

var nl = []byte{'\n'}

func countNL(b []byte) int {
	n := 0
	for {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			break
		}
		n++
		b = b[i+1:]
	}
	return n
}

func (g *Grep) Reader(r io.Reader, name string) {
	//log.Println("Grep starting.")
	if g.buf == nil {
		g.buf = make([]byte, 1<<20)
	}
	g.Match = false
	var (
		buf        = g.buf[:0]
		lineno     = 1
		prefix     = ""
		beginText  = true
		endText    = false
	)
	prefix = name + ":"
	for {
		n, err := io.ReadFull(r, buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		end := len(buf)
		if err == nil {
			end = bytes.LastIndex(buf, nl) + 1
		} else {
			endText = true
		}
		chunkStart := 0
		for chunkStart < end {
			m1 := g.Regexp.Match(buf[chunkStart:end], beginText, endText) + chunkStart
			beginText = false
			if m1 < chunkStart {
				break
			}
			lineStart := bytes.LastIndex(buf[chunkStart:m1], nl) + 1 + chunkStart
			lineEnd := m1 + 1
			if lineEnd > end {
				lineEnd = end
			}
			lineno += countNL(buf[chunkStart:lineStart])
			line := buf[lineStart:lineEnd]

			g.Stdout <- fmt.Sprintf("%s%d:%s", prefix, lineno, line)
			g.Match = true
			
			lineno++

			chunkStart = lineEnd
		}
		g.Matched <- g.Match
		if err == nil {
			lineno += countNL(buf[chunkStart:end])
		}
		n = copy(buf, buf[end:])
		buf = buf[:n]
		select {
			case <- g.Stop:
				//log.Println("Grep stopped.")
				return
			default:
		}
		if len(buf) == 0 && err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				fmt.Fprintf(g.Stderr, "%s: %v\n", name, err)
			}
			break
		}
	}
	//log.Println("Grep waiting for stop.")
	<- g.Stop
	//log.Println("Grep stopped.")
}

// kate: tab-width 4; indent-width 4;