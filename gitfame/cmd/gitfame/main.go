//go:build !solution

package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

type Statistic struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Commits int    `json:"commits"`
	Files   int    `json:"files"`
}

type Lang struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Extensions []string `json:"extensions"`
}

func loadLanguageExtensions(path string) ([]Lang, error) {
	var result []Lang
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(file, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func goodExtension(file string, extensions []string) bool {
	if extensions == nil {
		return true
	}
	fileExtension := filepath.Ext(file)
	for _, ext := range extensions {
		if fileExtension == ext {
			return true
		}
	}
	return false
}

func goodLanguage(file string, languages []string, languageExtensions []Lang) bool {
	if languages == nil {
		return true
	}
	knownLanguages := 0
	fileExtension := filepath.Ext(file)
	for _, language := range languages {
		for _, l := range languageExtensions {
			if language == strings.ToLower(l.Name) {
				knownLanguages++
				for _, ext := range l.Extensions {
					if fileExtension == ext {
						return true
					}
				}
			}
		}
	}
	return knownLanguages == 0
}

func forbiddenPattern(file string, forbiddenPatterns []string) bool {
	if forbiddenPatterns == nil {
		return false
	}
	for _, pattern := range forbiddenPatterns {
		match, _ := filepath.Match(pattern, file)
		if match {
			return true
		}
	}
	return false
}

func goodPattern(file string, allowedPatterns []string) bool {
	if allowedPatterns == nil {
		return true
	}
	for _, pattern := range allowedPatterns {
		match, _ := filepath.Match(pattern, file)
		if match {
			return true
		}
	}
	return false
}

func toInt(str string) int {
	val, err := strconv.Atoi(str)
	if err != nil {
		return 0
	}
	return val
}

func main() {
	curDir, _ := os.Getwd()

	repository := flag.String("repository", curDir, "Repository")
	revision := flag.String("revision", "HEAD", "Commit")
	order := flag.String("order-by", "lines", "Order")
	useCommitter := flag.Bool("use-committer", false, "Use comitter")
	resFormat := flag.String("format", "tabular", "Result format")
	extFlag := flag.String("extensions", "", "Allowed extensions")
	langFlag := flag.String("languages", "", "Allowed languages")
	excludeFlag := flag.String("exclude", "", "Forbidden patterns")
	restrictFlag := flag.String("restrict-to", "", "Allowed patterns")

	flag.Parse()

	var extensions []string
	extensions = nil
	if *extFlag != "" {
		extensions = strings.Split(*extFlag, ",")
	}
	var languages []string
	languages = nil
	if *langFlag != "" {
		languages = strings.Split(*langFlag, ",")
	}
	var forbiddenPatterns []string
	forbiddenPatterns = nil
	if *excludeFlag != "" {
		forbiddenPatterns = strings.Split(*excludeFlag, ",")
	}
	var allowedPatterns []string
	allowedPatterns = nil
	if *restrictFlag != "" {
		allowedPatterns = strings.Split(*restrictFlag, ",")
	}

	languageExtensions, err := loadLanguageExtensions("../../configs/language_extensions.json")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error while loading ext", err)
	}

	cmd := exec.Command("git", "ls-tree", "-r", "--name-only", *revision)
	cmd.Dir = *repository
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error in ls-tree")
		os.Exit(1)
	}
	files := strings.Split(string(output), "\n")
	files = files[:len(files)-1]
	var goodFiles []string
	for _, file := range files {
		if goodExtension(file, extensions) && goodLanguage(file, languages, languageExtensions) &&
			!forbiddenPattern(file, forbiddenPatterns) && goodPattern(file, allowedPatterns) {
			goodFiles = append(goodFiles, file)
		}
	}

	statMap := make(map[string]*Statistic)
	commitSet := make(map[string]struct{})
	commitAuthor := make(map[string]string)
	for _, file := range goodFiles {
		//path := filepath.Join(*repository, file)
		cmd := exec.Command("git", "blame", file, "--porcelain", *revision)
		cmd.Dir = *repository
		output, err := cmd.Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error in blame:", err)
			os.Exit(7)
		}
		if len(string(output)) == 0 {
			var commit string
			logCmd := exec.Command("git", "log", "-n", "1", *revision, "--", file)
			logCmd.Dir = *repository
			logOutput, err := logCmd.Output()
			if err != nil {
				os.Exit(7)
			}

			empLines := strings.Split(string(logOutput), "\n")
			for _, line := range empLines {
				if strings.HasPrefix(line, "commit ") {
					tokens := strings.Split(line, " ")
					commit = tokens[1]

				}
				if strings.HasPrefix(line, "Author: ") {
					str := strings.TrimPrefix(line, "Author: ")
					author := str[:strings.LastIndex(str, "<")-1]
					if _, ok := statMap[author]; !ok {
						statMap[author] = &Statistic{Name: author}
					}
					statMap[author].Files++
					if _, ok := commitSet[commit]; !ok {
						commitSet[commit] = struct{}{}
						commitAuthor[commit] = author
						statMap[author].Commits++
					}
				}
			}
			continue
		}

		lines := strings.Split(string(output), "\n")
		author := "emptyAuthor"
		authorSet := make(map[string]struct{})
		var lastCommit string
		for _, line := range lines {
			if strings.HasPrefix(line, "\t") {
				statMap[commitAuthor[lastCommit]].Lines++
			} else {
				tokens := strings.Split(line, " ")
				if !strings.HasPrefix(line, "author") && !strings.HasPrefix(line, "committer") &&
					!strings.HasPrefix(line, "summary") && !strings.HasPrefix(line, "boundary") &&
					!strings.HasPrefix(line, "filename") && !strings.HasPrefix(line, "previous") {
					lastCommit = tokens[0]
				}
				if strings.HasPrefix(line, "author ") {
					author = strings.TrimPrefix(line, "author ")
				}
				if strings.HasPrefix(line, "committer ") && *useCommitter {
					author = strings.TrimPrefix(line, "committer ")
				}
				if strings.HasPrefix(line, "filename ") {
					if _, ok := statMap[author]; !ok {
						statMap[author] = &Statistic{Name: author}
					}
					if _, ok := authorSet[author]; !ok {
						authorSet[author] = struct{}{}
						statMap[author].Files++
					}
					if _, ok := commitSet[lastCommit]; !ok {
						//fmt.Println(lastCommit)
						commitSet[lastCommit] = struct{}{}
						statMap[author].Commits++
						commitAuthor[lastCommit] = author
					}
				}
			}
		}
	}

	var statsSlice []Statistic
	for _, stats := range statMap {
		statsSlice = append(statsSlice, *stats)
	}

	switch *order {
	case "lines":
		sort.Slice(statsSlice, func(i, j int) bool {
			if statsSlice[i].Lines == statsSlice[j].Lines {
				if statsSlice[i].Commits == statsSlice[j].Commits {
					if statsSlice[i].Files == statsSlice[j].Files {
						return statsSlice[i].Name < statsSlice[j].Name
					}
					return statsSlice[i].Files > statsSlice[j].Files
				}
				return statsSlice[i].Commits > statsSlice[j].Commits
			}
			return statsSlice[i].Lines > statsSlice[j].Lines
		})
	case "commits":
		sort.Slice(statsSlice, func(i, j int) bool {
			if statsSlice[i].Commits == statsSlice[j].Commits {
				if statsSlice[i].Lines == statsSlice[j].Lines {
					if statsSlice[i].Files == statsSlice[j].Files {
						return statsSlice[i].Name < statsSlice[j].Name
					}
					return statsSlice[i].Files > statsSlice[j].Files
				}
				return statsSlice[i].Lines > statsSlice[j].Lines
			}
			return statsSlice[i].Commits > statsSlice[j].Commits
		})
	case "files":
		sort.Slice(statsSlice, func(i, j int) bool {
			if statsSlice[i].Files == statsSlice[j].Files {
				if statsSlice[i].Lines == statsSlice[j].Lines {
					if statsSlice[i].Commits == statsSlice[j].Commits {
						return statsSlice[i].Name < statsSlice[j].Name
					}
					return statsSlice[i].Commits > statsSlice[j].Commits
				}
				return statsSlice[i].Lines > statsSlice[j].Lines
			}
			return statsSlice[i].Files > statsSlice[j].Files
		})
	default:
		fmt.Println("Incorrect option!")
		os.Exit(1)
	}

	switch *resFormat {
	case "tabular":
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', 0)
		defer tw.Flush()
		fmt.Fprintf(tw, "Name\tLines\tCommits\tFiles\n")
		for _, stats := range statsSlice {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", stats.Name, stats.Lines, stats.Commits, stats.Files)
		}
	case "csv":
		csvWriter := csv.NewWriter(os.Stdout)
		defer csvWriter.Flush()
		err := csvWriter.Write([]string{"Name", "Lines", "Commits", "Files"})
		if err != nil {
			os.Exit(1)
		}
		for _, stats := range statsSlice {
			err := csvWriter.Write([]string{stats.Name, fmt.Sprintf("%d", stats.Lines), fmt.Sprintf("%d", stats.Commits), fmt.Sprintf("%d", stats.Files)})
			if err != nil {
				os.Exit(1)
			}
		}
	case "json":
		jsonStats, err := json.Marshal(statsSlice)
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(1)
		}
		fmt.Println(string(jsonStats))
	case "json-lines":
		for _, stats := range statsSlice {
			jsonStats, err := json.Marshal(stats)
			if err != nil {
				fmt.Println("Error:", err)
				os.Exit(1)
			}
			fmt.Println(string(jsonStats))
		}
	default:
		fmt.Println("Invalid format option")
		os.Exit(1)
	}
}
