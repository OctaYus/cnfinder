package main

import (
        "bufio"
        "context"
        "flag"
        "fmt"
        "io"
        "log"
        "net"
        "os"
        "path/filepath"
        "runtime"
        "strings"
        "sync"
        "time"
)

const (
        COLOR_RED   = "\033[31m"
        COLOR_GREEN = "\033[32m"
        COLOR_CYAN  = "\033[36m"
        COLOR_RESET = "\033[0m"
)

const banner = `
  ____       _____ _           _
 / ___|_ __ |  ___(_)_ __   __| | ___ _ __
| |   | '_ \| |_  | | '_ \ / _' |/ _ \ '__|
| |___| | | |  _| | | | | | (_| |  __/ |
 \____|_| |_|_|   |_|_| |_|\__,_|\___|_|

`

type result struct {
        sub, cname string
        err        error
}

func printColored(color, format string, args ...interface{}) {
        fmt.Printf("%s%s%s\n", color, fmt.Sprintf(format, args...), COLOR_RESET)
}

func normalize(name string) string {
        return strings.TrimSuffix(strings.TrimSpace(name), ".")
}

func stripScheme(s string) string {
        s = strings.TrimSpace(s)
        if strings.HasPrefix(s, "http://") {
                return s[7:]
        }
        if strings.HasPrefix(s, "https://") {
                return s[8:]
        }
        // also strip any trailing slashes from URLs like example.com/
        return strings.TrimSuffix(s, "/")
}

func resolveCNAME(name string, timeout time.Duration) (string, error) {
        ctx, cancel := context.WithTimeout(context.Background(), timeout)
        defer cancel()
        cname, err := net.DefaultResolver.LookupCNAME(ctx, name)
        if err != nil {
                return "", err
        }
        return normalize(cname), nil
}

func main() {
        // Print ASCII banner in cyan
        fmt.Print(COLOR_CYAN, banner, COLOR_RESET)

        // Flags
        inputFile := flag.String("l", "", "input file with one subdomain per line, or '-' for stdin")
        outputFile := flag.String("o", "cnames.txt", "output file (each line: domain > cname)")
        timeout := flag.Duration("timeout", 5*time.Second, "DNS query timeout, e.g. 3s, 500ms")
        numWorkers := flag.Int("t", runtime.NumCPU(), "number of concurrent workers (default: CPUs)")
        appendMode := flag.Bool("a", false, "append to output instead of truncating")

        // Custom usage to show short examples
        flag.Usage = func() {
                fmt.Fprintf(flag.CommandLine.Output(), "Usage of %s:\n", os.Args[0])
                flag.PrintDefaults()
                fmt.Fprintln(flag.CommandLine.Output(), "\nExamples:")
                fmt.Fprintln(flag.CommandLine.Output(), "  cnfinder -l subdomains.txt -o results.txt")
                fmt.Fprintln(flag.CommandLine.Output(), "  cat subdomains.txt | cnfinder -l - -o results.txt")
        }

        flag.Parse()

        // Decide input source and validate
        var in io.Reader
        // If user explicitly passed "-" read stdin regardless of whether it's a terminal.
        if *inputFile == "-" {
                in = os.Stdin
                log.Printf("Reading subdomains from stdin (explicit '-')")
        } else if *inputFile != "" {
                // user provided a filename
                f, err := os.Open(*inputFile)
                if err != nil {
                        log.Fatalf("%sError opening input file %s: %v%s\n", COLOR_RED, *inputFile, err, COLOR_RESET)
                }
                defer f.Close()
                in = f
        } else {
                // No -l provided: check if there's piped data on stdin
                stat, err := os.Stdin.Stat()
                if err != nil {
                        // If we can't stat stdin treat as nothing provided
                        printColored(COLOR_RED, "[-] Unable to determine stdin state: %v", err)
                        flag.Usage()
                        os.Exit(1)
                }
                // If stdin is a char device (terminal) then nothing is piped
                if (stat.Mode() & os.ModeCharDevice) != 0 {
                        printColored(COLOR_RED, "[-] No input specified. Provide -l <file> or pipe data to stdin.")
                        flag.Usage()
                        os.Exit(1)
                }
                // Otherwise stdin has data being piped
                in = os.Stdin
                log.Printf("Reading subdomains from stdin (piped data)")
        }

        // Ensure output dir exists if necessary
        outDir := filepath.Dir(*outputFile)
        if outDir != "" && outDir != "." {
                if err := os.MkdirAll(outDir, 0o755); err != nil {
                        log.Fatalf("%sError creating output directory %s: %v%s\n", COLOR_RED, outDir, err, COLOR_RESET)
                }
        }

        // Open output file
        var of *os.File
        var err error
        if *appendMode {
                of, err = os.OpenFile(*outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        } else {
                of, err = os.Create(*outputFile)
        }
        if err != nil {
                log.Fatalf("%sError opening output file %s: %v%s\n", COLOR_RED, *outputFile, err, COLOR_RESET)
        }
        defer of.Close()

        // Read/parse lines and sanitize
        subs := make([]string, 0, 1000)
        scanner := bufio.NewScanner(in)
        for scanner.Scan() {
                line := strings.TrimSpace(scanner.Text())
                if line == "" || strings.HasPrefix(line, "#") {
                        continue
                }
                clean := stripScheme(line)
                if clean == "" {
                        continue
                }
                subs = append(subs, clean)
        }
        if err := scanner.Err(); err != nil {
                log.Fatalf("%sError reading input: %v%s\n", COLOR_RED, err, COLOR_RESET)
        }

        // If there are no subs after parsing, exit early
        if len(subs) == 0 {
                printColored(COLOR_CYAN, "[-] No subdomains found in input, exiting.")
                return
        }

        // Set up worker pool
        jobs := make(chan string)
        results := make(chan result)
        var wg sync.WaitGroup

        for i := 0; i < *numWorkers; i++ {
                wg.Add(1)
                go func() {
                        defer wg.Done()
                        for sub := range jobs {
                                cname, err := resolveCNAME(sub, *timeout)
                                results <- result{sub, cname, err}
                        }
                }()
        }

        // Output processing goroutine
        var outputWg sync.WaitGroup
        outputWg.Add(1)
        go func() {
                defer outputWg.Done()
                for res := range results {
                        sub, cname, err := res.sub, res.cname, res.err
                        if err != nil {
                                if dnsErr, ok := err.(*net.DNSError); ok {
                                        if dnsErr.IsNotFound {
                                                printColored(COLOR_CYAN, "[-] %s does not exist", sub)
                                        } else if dnsErr.IsTimeout {
                                                printColored(COLOR_RED, "[-] Timeout resolving %s", sub)
                                        } else {
                                                printColored(COLOR_RED, "[-] Error checking CNAME for %s: %v", sub, err)
                                        }
                                } else {
                                        printColored(COLOR_RED, "[-] Error checking CNAME for %s: %v", sub, err)
                                }
                                continue
                        }
                        if normalize(cname) == normalize(sub) {
                                printColored(COLOR_RED, "[-] No CNAME record found for %s", sub)
                                continue
                        }
                        if _, werr := fmt.Fprintf(of, "%s > %s\n", sub, cname); werr != nil {
                                printColored(COLOR_RED, "[-] Failed writing result for %s: %v", sub, werr)
                                continue
                        }
                        printColored(COLOR_GREEN, "[+] %s > %s", sub, cname)
                }
        }()

        // Feed jobs to workers
        go func() {
                for _, sub := range subs {
                        jobs <- sub
                }
                close(jobs)
        }()

        // Wait for all workers to finish
        wg.Wait()
        close(results)
        outputWg.Wait()
}
