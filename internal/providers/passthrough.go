package providers

// passthroughTranslation is embedded by providers whose native API is
// already OpenAI-compatible in shape (OpenAI itself, Mistral): no
// request/response body translation is needed at all, only routing
// (Target) and authentication (Authenticate) differ between them.
type passthroughTranslation struct{}

func (passthroughTranslation) TranslateRequest(body []byte) ([]byte, error) { return body, nil }

func (passthroughTranslation) TranslateResponse(body []byte) ([]byte, error) { return body, nil }

func (passthroughTranslation) NewStreamTranslator() StreamTranslator {
	return passthroughStreamTranslator{}
}

// passthroughStreamTranslator forwards bytes unchanged: no buffering, no
// per-chunk cost, since an already-OpenAI-shaped stream needs no
// translation before it reaches pii.SSEUnmasker.
type passthroughStreamTranslator struct{}

func (passthroughStreamTranslator) Feed(chunk []byte) []byte { return chunk }

func (passthroughStreamTranslator) Flush() []byte { return nil }
