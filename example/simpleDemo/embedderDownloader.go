package main

import (
    "fmt"
    "log"
    "os/exec"
    "runtime"
    "regexp"
    "net/http"
    "os"
    "bufio"
    "io"
    "io/ioutil"
    "encoding/json"
    "time"
    "path/filepath"
    "strconv"
    "archive/zip"
    "strings"
)

// Unzip will decompress a zip archive, moving all files and folders 
// within the zip file (parameter 1) to an output directory (parameter 2).
func unzip(src string, dest string) ([]string, error) {

    var filenames []string

    r, err := zip.OpenReader(src)
    if err != nil {
        return filenames, err
    }
    defer r.Close()

    for _, f := range r.File {

        rc, err := f.Open()
        if err != nil {
            return filenames, err
        }
        defer rc.Close()

        // Store filename/path for returning and using later on
        fpath := filepath.Join(dest, f.Name)

        // Check for ZipSlip. More Info: http://bit.ly/2MsjAWE
        if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
            return filenames, fmt.Errorf("%s: illegal file path", fpath)
        }

        filenames = append(filenames, fpath)

        if f.FileInfo().IsDir() {

            // Make Folder
            os.MkdirAll(fpath, os.ModePerm)

        } else {

            // Make File
            if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
                return filenames, err
            }

            outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
            if err != nil {
                return filenames, err
            }

            _, err = io.Copy(outFile, rc)

            // Close the file without defer to close before next iteration of loop
            outFile.Close()

            if err != nil {
                return filenames, err
            }

        }
    }
    return filenames, nil
}


func askForConfirmation() bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("Would you like to overwrite the previously downloaded engine [Y/n] : ")

		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		response = strings.ToLower(strings.TrimSpace(response))

		if response == "y" || response == "yes" {
			return true
		} else if response == "n" || response == "no" {
			return false
		}
	}
}

// Function to prind download percent completion
func printDownloadPercent(done chan int64, path string, total int64) {

	var stop bool = false

	for {
		select {
		case <-done:
			stop = true
		default:

			file, err := os.Open(path)
			if err != nil {
				log.Fatal(err)
			}

			fi, err := file.Stat()
			if err != nil {
				log.Fatal(err)
			}

			size := fi.Size()

			if size == 0 {
				size = 1
			}

			var percent float64 = float64(size) / float64(total) * 100
            
            // We use `\033[2K\r` to avoid carriage return, it will print above previous.
            fmt.Printf("\033[2K\r %.0f %% / 100 %%", percent)
		}

		if stop {
			break
		}

		time.Sleep(time.Second)
	}
}

// Function to download file with given path and url.
func downloadFile(filepath string, url string) error {

    // Print download url in case user needs it.
	fmt.Printf("Downloading file from %s\n", url)

    if _, err := os.Stat(filepath); !os.IsNotExist(err) {
        if !askForConfirmation(){
            fmt.Printf("Leaving.\n")
            os.Exit(0)
        }
    }
    start := time.Now()
    
    // Create the file
    out, err := os.Create(filepath)
    if err != nil {
        return err
    }
    defer out.Close()

    // Get the data
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    size, err := strconv.Atoi(resp.Header.Get("Content-Length"))

    done := make(chan int64)

	go printDownloadPercent(done, filepath, int64(size))


    // Write the body to file
    n, err := io.Copy(out, resp.Body)
    if err != nil {
        return err
    }

    done <- n

	elapsed := time.Since(start)
	log.Printf("\033[2K\rDownload completed in %s", elapsed)

    return nil
}

func main() {
    // Execute flutter command to retrieve the version
	out, err := exec.Command("flutter","--version").Output()
    if err != nil {
        log.Fatal(err)
    }

    // Get working directory
    dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

    re := regexp.MustCompile(`Engine • revision (\w{10})`)
    shortRevision := re.FindStringSubmatch(string(out))[1]

    url := fmt.Sprintf("https://api.github.com/search/commits?q=%s", shortRevision)

    // This part is used to retrieve the full hash 
    req, err := http.NewRequest("GET", os.ExpandEnv(url), nil)
    if err != nil {
        // handle err
        log.Fatal(err)
    }
    req.Header.Set("Accept", "application/vnd.github.cloak-preview")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        // handle err
        log.Fatal(err)
    }

    body, err := ioutil.ReadAll(resp.Body)
    defer resp.Body.Close()
    if err != nil {
        log.Fatal(err)
    }

    // We define a struct to build JSON object from the response
    hashResponse := struct {
		Items             []struct {
			Sha string `json:"sha"`
		} `json:"items"`
	}{}

	err2 := json.Unmarshal(body, &hashResponse)
    if err2 != nil {
        // handle err
        log.Fatal(err2)
    }

	var platform = "undefined"
    var downloadUrl = ""
	
    // Retrieve the OS and set variable to retrieve correct flutter embedder
    switch runtime.GOOS {
    case "darwin":
        platform = "darwin-x64"
        downloadUrl = fmt.Sprintf("https://storage.googleapis.com/flutter_infra/flutter/%s/%s/FlutterEmbedder.framework.zip", hashResponse.Items[0].Sha, platform)

    case "linux":
        platform = "linux-x64"
        downloadUrl = fmt.Sprintf("https://storage.googleapis.com/flutter_infra/flutter/%s/%s/%s-embedder", hashResponse.Items[0].Sha, platform, platform)

    case "windows":
        platform = "windows-x64"
        downloadUrl = fmt.Sprintf("https://storage.googleapis.com/flutter_infra/flutter/%s/%s/%s-embedder", hashResponse.Items[0].Sha, platform, platform)

    default:
        log.Fatal("OS not supported")
    }

    err3 := downloadFile(dir + "/.build/temp.zip", downloadUrl)
    if err3 != nil {
        log.Fatal(err3)
    } else{
        fmt.Printf("Downloaded embedder for %s platform, matching version : %s\n", platform, hashResponse.Items[0].Sha)
    }

    _, err = unzip(".build/temp.zip", dir + "/.build/")
    if err != nil {
        log.Fatal(err)
    }

    switch platform{
    case "darwin-x64":       
        _, err = unzip(".build/FlutterEmbedder.framework.zip", dir + "/FlutterEmbedder.framework")
        if err != nil {
            log.Fatal(err)
        }

    case "linux-x64":
        err := os.Rename("libflutter_engine.so", dir + "/libflutter_engine.so")
        if err != nil {
            log.Fatal(err)
        }

    case "windows-x64":
        err := os.Rename("flutter_engine.dll", dir + "/flutter_engine.dll")
        if err != nil {
            log.Fatal(err)
        }

    }
    fmt.Printf("Unzipped files and moved them to correct repository.\n")

    fmt.Printf("Done.\n")

}