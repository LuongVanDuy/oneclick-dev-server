package recoverytool

type Status struct {
	State         string `json:"state"`
	Message       string `json:"message"`
	Detail        string `json:"detail,omitempty"`
	URL           string `json:"url,omitempty"`
	Version       string `json:"version,omitempty"`
	DataPath      string `json:"dataPath,omitempty"`
	Ready         bool   `json:"ready"`
	Running       bool   `json:"running"`
	SetupRequired bool   `json:"setupRequired"`
}

type Progress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
}

type ImportPlan struct {
	SourcePath      string `json:"sourcePath"`
	DestinationPath string `json:"destinationPath"`
	Files           int    `json:"files"`
	Bytes           int64  `json:"bytes"`
	Profiles        int    `json:"profiles"`
	Secrets         int    `json:"secrets"`
	ExistingFiles   int    `json:"existingFiles"`
	Conflicts       int    `json:"conflicts"`
	Ready           bool   `json:"ready"`
	Message         string `json:"message"`
	Detail          string `json:"detail,omitempty"`
}

type ImportProgress struct {
	Stage      string `json:"stage"`
	Message    string `json:"message"`
	Percent    int    `json:"percent"`
	FilesDone  int    `json:"filesDone"`
	FilesTotal int    `json:"filesTotal"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
}

type ImportResult struct {
	Success         bool   `json:"success"`
	Message         string `json:"message"`
	Detail          string `json:"detail,omitempty"`
	Files           int    `json:"files"`
	Bytes           int64  `json:"bytes"`
	Skipped         int    `json:"skipped"`
	DestinationPath string `json:"destinationPath,omitempty"`
}

type Reporter func(Progress)
type ImportReporter func(ImportProgress)
