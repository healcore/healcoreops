// Package maintenance — ремонт и обслуживание Windows: проверка системных
// файлов, сброс компонентов обновления, сеть и DNS. Длинные операции
// выполняются в фоне, их вывод построчно отдаётся интерфейсу.
package maintenance

import "sync"

// Action — операция обслуживания.
type Action struct {
	ID    string `json:"id"`
	Group string `json:"group"` // repair | network
	Title string `json:"title"`
	Desc  string `json:"desc"`
	Admin bool   `json:"admin"`
	Mins  int    `json:"mins"` // ориентировочная длительность, мин
}

// Job — состояние текущей фоновой операции.
type Job struct {
	Running bool     `json:"running"`
	Action  string   `json:"action"`
	Title   string   `json:"title"`
	Lines   []string `json:"lines"`
	Done    bool     `json:"done"`
	OK      bool     `json:"ok"`
}

var (
	jmu sync.Mutex
	job Job
)

// Status возвращает копию состояния операции.
func Status() Job {
	jmu.Lock()
	defer jmu.Unlock()
	cp := job
	cp.Lines = append([]string(nil), job.Lines...)
	return cp
}

func emit(line string) {
	jmu.Lock()
	job.Lines = append(job.Lines, line)
	if len(job.Lines) > 500 {
		job.Lines = job.Lines[len(job.Lines)-500:]
	}
	jmu.Unlock()
}

// run запускает операцию в фоне, если другая не выполняется.
func run(id, title string, fn func(emit func(string)) error) error {
	jmu.Lock()
	if job.Running {
		jmu.Unlock()
		return errBusy
	}
	job = Job{Running: true, Action: id, Title: title}
	jmu.Unlock()

	go func() {
		err := fn(emit)
		jmu.Lock()
		job.Running = false
		job.Done = true
		job.OK = err == nil
		if err != nil {
			job.Lines = append(job.Lines, "× "+err.Error())
		}
		jmu.Unlock()
	}()
	return nil
}

// StartJob запускает произвольную фоновую операцию с общим журналом вывода
// (используется также менеджером программ). Одновременно выполняется одна операция.
func StartJob(id, title string, fn func(emit func(string)) error) error { return run(id, title, fn) }
