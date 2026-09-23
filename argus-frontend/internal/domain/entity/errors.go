package entity

import "errors"

// ErrNotReady означает, что данных ещё нет: бэкенд не поднялся, не отвечает
// или схема в нём ещё не создана.
//
// Порядок старта collector и web не гарантирован (ТЗ §11), поэтому такая
// ситуация штатная. Дашборд обязан показать понятный экран ожидания, а не 500.
var ErrNotReady = errors.New("argus-backend ещё не готов или недоступен")

// ErrNotFound - цели с таким именем бэкенд не отслеживает.
var ErrNotFound = errors.New("цель не отслеживается")

var ErrBackendTooOld = errors.New("argus-backend устарел: нет order=desc и X-Total-Count, обновите его")
