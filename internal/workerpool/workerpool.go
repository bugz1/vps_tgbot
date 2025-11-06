package workerpool

import (
	"context"
	"sync"
	"time"
	
	"tgbot/pkg/logger"
)

// Task представляет собой задачу для выполнения в worker pool
type Task struct {
	ID      string
	Handler func() (interface{}, error)
	Timeout time.Duration
}

// Result представляет результат выполнения задачи
type Result struct {
	ID    string
	Value interface{}
	Error error
}

// WorkerPool представляет пул воркеров для выполнения задач
type WorkerPool struct {
	workersCount int
	taskQueue    chan Task
	resultQueue  chan Result
	wg           sync.WaitGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

// New создает новый worker pool с указанным количеством воркеров
func New(workersCount int) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	
	wp := &WorkerPool{
		workersCount: workersCount,
		taskQueue:    make(chan Task, 100), // Буферизованный канал для задач
		resultQueue:  make(chan Result, 100), // Буферизованный канал для результатов
		ctx:          ctx,
		cancel:       cancel,
	}
	
	// Запускаем воркеры
	wp.startWorkers()
	
	return wp
}

// startWorkers запускает воркеры в отдельных goroutines
func (wp *WorkerPool) startWorkers() {
	for i := 0; i < wp.workersCount; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// worker выполняет задачи из очереди
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	
	for {
		select {
		case task := <-wp.taskQueue:
			// Создаем контекст с таймаутом для задачи
			ctx, cancel := context.WithTimeout(wp.ctx, task.Timeout)
			
			// Канал для получения результата задачи
			resultChan := make(chan Result, 1)
			
			// Выполняем задачу в отдельной goroutine
			go func() {
				value, err := task.Handler()
				resultChan <- Result{
					ID:    task.ID,
					Value: value,
					Error: err,
				}
			}()
			
			// Ждем результат или таймаут
			select {
			case result := <-resultChan:
				// Логируем успешное выполнение задачи
				logger.Log(logger.Debug, "workerpool.task_completed", map[string]interface{}{
					"task_id": task.ID,
					"timeout": task.Timeout,
				})
				wp.resultQueue <- result
				cancel()
			case <-ctx.Done():
				// Логируем таймаут задачи
				logger.Log(logger.Warn, "workerpool.task_timeout", map[string]interface{}{
					"task_id": task.ID,
					"timeout": task.Timeout,
					"error":   ctx.Err().Error(),
				})
				wp.resultQueue <- Result{
					ID:    task.ID,
					Value: nil,
					Error: ctx.Err(),
				}
				cancel()
			}
		case <-wp.ctx.Done():
			return
		}
	}
}

// Submit отправляет задачу в пул для выполнения
func (wp *WorkerPool) Submit(task Task) {
	select {
	case wp.taskQueue <- task:
		// Логируем отправку задачи в пул
		logger.Log(logger.Debug, "workerpool.task_submitted", map[string]interface{}{
			"task_id": task.ID,
			"timeout": task.Timeout,
		})
	case <-wp.ctx.Done():
		// Если контекст отменен, отправляем ошибку в канал результатов
		logger.Log(logger.Error, "workerpool.submit_cancelled", map[string]interface{}{
			"task_id": task.ID,
			"error":   wp.ctx.Err().Error(),
		})
		wp.resultQueue <- Result{
			ID:    task.ID,
			Value: nil,
			Error: wp.ctx.Err(),
		}
	}
}

// Results возвращает канал с результатами выполнения задач
func (wp *WorkerPool) Results() <-chan Result {
	return wp.resultQueue
}

// Close останавливает worker pool
func (wp *WorkerPool) Close() {
	wp.cancel()
	wp.wg.Wait()
	close(wp.taskQueue)
	close(wp.resultQueue)
}