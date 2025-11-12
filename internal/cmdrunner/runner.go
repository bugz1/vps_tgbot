package cmdrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"tgbot/pkg/logger"

	"github.com/spf13/viper"
)

// PasswordRequester интерфейс для запроса пароля через Telegram
type PasswordRequester interface {
	RequestPassword(chatID int64) (string, error)
}

// RunOptions опции для выполнения команды
type RunOptions struct {
	Timeout            time.Duration
	Attempts           int
	Password           string
	PasswordFromEnv    bool
	PasswordFromConfig bool
	Requester          PasswordRequester
	ChatID             int64
}

// RunWithRetries выполняет внешнюю команду с контекстным таймаутом и попытками (retries).
// cmdParts — полный срез частей команды (например: ["sudo", "docker", "exec", ...]).
// opts — опции выполнения команды
// Возвращает combined stdout+stderr и ошибку.
func RunWithRetries(ctx context.Context, cmdParts []string, opts RunOptions) (string, error) {
	var lastErr error

	if len(cmdParts) == 0 {
		return "", fmt.Errorf("пустой список частей команды")
	}

	// Заполняем значения по умолчанию из конфигурации, если они не заданы
	if opts.Timeout <= 0 {
		secs := viper.GetInt("cmdrunner.timeout_seconds")
		if secs <= 0 {
			secs = 10
		}
		opts.Timeout = time.Duration(secs) * time.Second
	}
	if opts.Attempts <= 0 {
		opts.Attempts = viper.GetInt("cmdrunner.attempts")
		if opts.Attempts <= 0 {
			opts.Attempts = 1
		}
	}

	// Пытаемся выполнить команду заданное количество раз
	for i := 0; i < opts.Attempts; i++ {
		attemptNum := i + 1
		// Создаем контекст с таймаутом для текущей попытки
		ctxTimeout, cancel := context.WithTimeout(ctx, opts.Timeout)
		// Создаем команду с контекстом
		cmd := exec.CommandContext(ctxTimeout, cmdParts[0], cmdParts[1:]...)

		// Если команда использует sudo — заранее подставим пароль ТОЛЬКО если он уже есть
		// (ENV/CONFIG/opts). Пароль у пользователя не запрашиваем заранее.
		hasSudo := len(cmdParts) > 0 && cmdParts[0] == "sudo"
		var prePwd string
		if hasSudo {
			prePwd = opts.Password
			if prePwd == "" && opts.PasswordFromEnv {
				prePwd = os.Getenv("SUDO_PASSWORD")
			}
			if prePwd == "" && opts.PasswordFromConfig {
				prePwd = viper.GetString("cmdrunner.sudo_password")
			}
			if prePwd != "" {
				// sudo -S будет читать пароль из stdin
				hasS := false
				for _, p := range cmdParts[1:] {
					if p == "-S" {
						hasS = true
						break
					}
				}
				if !hasS {
					parts := make([]string, 0, len(cmdParts)+1)
					parts = append(parts, "sudo", "-S")
					parts = append(parts, cmdParts[1:]...)
					cmd = exec.CommandContext(ctxTimeout, parts[0], parts[1:]...)
				}
				cmd.Stdin = strings.NewReader(prePwd + "\n")
			}
		}

		// Захватываем stdout и stderr отдельно, назначая буферы
		var stdoutBuf, stderrBuf bytes.Buffer
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf

		// Запускаем команду
		if err := cmd.Start(); err != nil {
			lastErr = fmt.Errorf("не удалось запустить команду: %v", err)
			// Логируем ошибку запуска команды
			logger.Log(logger.Warn, "cmdrunner.start_failed", logger.MaskSensitiveFields(map[string]interface{}{"cmd": strings.Join(cmdParts, " "), "attempt": attemptNum, "error": err.Error()}))
			// Простая задержка перед повторной попыткой
			cancel()
			time.Sleep(time.Duration(200*attemptNum) * time.Millisecond)
			continue
		}

		// Ожидаем завершения команды
		err := cmd.Wait()
		cancel()

		// Получаем результаты выполнения команды
		stdoutStr := stdoutBuf.String()
		stderrStr := stderrBuf.String()

		// Получаем код возврата команды
		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}

		// Логируем результат попытки (обрезаем большие выводы)
		maxLog := 2000
		outForLog := stdoutStr
		errForLog := stderrStr
		if len(outForLog) > maxLog {
			outForLog = outForLog[:maxLog] + "\n... (обрезано)"
		}
		if len(errForLog) > maxLog {
			errForLog = errForLog[:maxLog] + "\n... (обрезано)"
		}

		// Записываем лог о попытке выполнения команды
		logger.Log(logger.Debug, "cmdrunner.attempt", logger.MaskSensitiveFields(map[string]interface{}{
			"cmd":       strings.Join(cmdParts, " "),
			"attempt":   attemptNum,
			"exit_code": exitCode,
			"stdout":    outForLog,
			"stderr":    errForLog,
		}))

		// Если команда выполнена успешно, возвращаем результат
		if err == nil {
			// Успешное выполнение
			combined := stdoutStr
			if stderrStr != "" {
				combined = combined + "\n" + stderrStr
			}
			return combined, nil
		}

		// Определяем причину ошибки
		if ctxTimeout.Err() == context.DeadlineExceeded {
			lastErr = fmt.Errorf("таймаут после %s: %w", opts.Timeout, ctxTimeout.Err())
		} else {
			lastErr = fmt.Errorf("ошибка выполнения команды (попытка %d/%d): %v", attemptNum, opts.Attempts, err)
		}

		// Если это sudo и пароль не был задан заранее — проверим, не запросил ли sudo пароль
		if hasSudo && prePwd == "" {
			needPwd := strings.Contains(stderrStr, "password is required") ||
				strings.Contains(stderrStr, "a password is required") ||
				strings.Contains(stderrStr, "sudo:") && strings.Contains(stderrStr, "password") ||
				strings.Contains(stderrStr, "no tty present and no askpass program specified") ||
				strings.Contains(stderrStr, "try again.")
			if needPwd && opts.Requester != nil && opts.ChatID != 0 {
				// Запрашиваем пароль у пользователя и повторяем команду немедленно (в рамках той же попытки)
				pwd, reqErr := opts.Requester.RequestPassword(opts.ChatID)
				if reqErr != nil {
					lastErr = fmt.Errorf("ошибка запроса пароля через Telegram: %v", reqErr)
				} else if pwd != "" {
					// Готовим команду с -S и паролем в stdin
					parts := make([]string, 0, len(cmdParts)+1)
					parts = append(parts, "sudo", "-S")
					parts = append(parts, cmdParts[1:]...)
					retryCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
					var rOut, rErr bytes.Buffer
					retryCmd.Stdout = &rOut
					retryCmd.Stderr = &rErr
					retryCmd.Stdin = strings.NewReader(pwd + "\n")
					if runErr := retryCmd.Run(); runErr == nil {
						combined := rOut.String()
						if rErr.String() != "" {
							combined = combined + "\n" + rErr.String()
						}
						return combined, nil
					} else {
						// Обновим lastErr контекстом повторной ошибки
						lastErr = fmt.Errorf("ошибка после ввода пароля: %v (stderr: %s)", runErr, rErr.String())
					}
				}
			}
		}

		// Задержка перед следующей попыткой
		time.Sleep(time.Duration(200*attemptNum) * time.Millisecond)
	}

	return "", lastErr
}
