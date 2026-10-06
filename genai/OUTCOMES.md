# Observation outcomes

This contract defines local recording lifecycles; callers own business execution.

## Public API

- `type Outcome string`; поля `Status Outcome` вместо произвольных строк в OperationResult, ToolResult, Meta. Значения констант OperationStatusOK="ok", OperationStatusError="error", OperationStatusUnset="unset" сохраняются как значения текущей схемы, без legacy parser.
- `ResponseMetadata{Model string}`; `OperationResult.Response ResponseMetadata`. RequestModel определяется Operation.Model/Meta.RequestModel/request scope. Meta.ResponseModel допускается только как явно наблюдавшийся caller факт; никакого request/scope fallback.
- `ProviderAdapter.ParseResponse(any) (ResponseObservation,error)`; `ResponseObservation{Payload Payload,Usage Usage,Metadata ResponseMetadata,Status Outcome,ErrorType string}`. Adapter не хранит состояние. ParseRequest.Meta.ResponseModel сбрасывается перед применением actual response.
- `OperationHandle.Finish(OperationResult) error`, `Close() error`.
- `Recorder.StartTool(ctx,ToolCall,...ToolOption) (context.Context,*ToolHandle,error)`; `ToolHandle.Finish(ToolResult) error`, `Close() error`. Удалить ToolEnd и Runtime.RecordToolResult.
- `Tracker.StartRetrieval(ctx,name,RetrievalRequest,...ChildSpanOption) (context.Context,*RetrievalHandle,error)`; тот же метод Recorder (noop validate). `RetrievalResult` добавляет `Status Outcome, Err error, ErrorClass ErrorClass`. `RetrievalHandle.Finish(RetrievalResult) error`, `Close() error`. Старые StartRetrievalSpan/RecordRetrievalResult удалить.
- Все handles локальные mutex-protected: invalid Finish ничего не пишет и исправим; успешный Finish/Close закрывает один раз, далее ErrFinalized. Нулевая/отсутствующая handle — bounded diagnostic. Close завершает unknown без fabricated usage/result/chunks. Noop validation равна recording validation.

## Outcome table

| Status input | Error evidence | Exported status | OTel | error.type |
|---|---|---|---|---|
| ok | absent | ok | OK | absent |
| unset или empty | absent | unset | Unset | absent |
| error | absent | error | Error | unknown |
| error или empty | present | error | Error | bounded observed class |
| ok или explicit unset | present | diagnostic, no writes | no mutation | no mutation |
| any other enum | any | diagnostic, no writes | no mutation | no mutation |

Model ErrorType accepts exact bounded list (unknown,error,cancelled,deadline_exceeded,stream_aborted,timeout,rate_limit,quota_exceeded,validation,unavailable,permission_denied,unauthenticated,not_found,conflict,internal). Invalid raw classes reject rather than normalize. Tool/retrieval explicit ErrorClass must be exact allowed class; configured classifier may normalize unknown. No raw errors exported. Known cancellation/deadline derive from errors.Is only for tool/retrieval; model producer supplies known class. Async deferred events are independent linked observations rather than local I/O handles.

## Metrics and duration

Model operation duration and tool duration include consistent outcome dimensions. Metadata normalizers receive actual request and response values separately; absent response omitted. Live elapsed uses time.Now/time.Since monotonic, producer cannot supply duration. Completed model/streaming records remain marked completed and carry supplied known duration as metric, while span covers recording interval only. Physical attempts account known final resources even on error; logical summaries skip accounting. Streaming update never commits totals, explicit final abort may supply usage.

All incoming slices copied or consumed without modification. Payload-policy errors do not invent business outcome; result validation before final writes, payload diagnostics remain bounded. No scheduler/retries/business operation control.
