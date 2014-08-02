package grohl

// A Context holds default key/value data that merges with the data every Log()
// call receives.
type Context struct {
	data          Data
	Logger        Logger
	TimeUnit      string
	ErrorReporter ErrorReporter
	*_statter
}

// Log merges the given data with the Context's data, and passes it to the
// Logger.
func (c *Context) Log(data Data) error {
	return c.Logger.Log(c.Merge(data))
}

func (c *Context) log(data Data) error {
	return c.Logger.Log(data)
}

// New creates a duplicate Context object, merging the given data with the
// Context's data.
func (c *Context) New(data Data) *Context {
	return newContext(c.Merge(data), c.Logger, c.TimeUnit, c.ErrorReporter, c._statter)
}

// Add adds the key and value to the Context's data.
func (c *Context) Add(key string, value interface{}) {
	c.data[key] = value
}

// Merge combines the given key/value data with the Context's data.  If no data
// is given, a clean duplicate of the Context's data is returned.
func (c *Context) Merge(data Data) Data {
	if data == nil {
		return dupeMaps(c.data)
	} else {
		return dupeMaps(c.data, data)
	}
}

// Delete removes the key from the Context's data.
func (c *Context) Delete(key string) {
	delete(c.data, key)
}

func dupeMaps(maps ...Data) Data {
	merged := make(Data)
	for _, orig := range maps {
		for key, value := range orig {
			merged[key] = value
		}
	}
	return merged
}

func newContext(data Data, logger Logger, timeunit string, reporter ErrorReporter, statter *_statter) *Context {
	return &Context{
		data:          data,
		Logger:        logger,
		TimeUnit:      timeunit,
		ErrorReporter: reporter,
		_statter:      statter,
	}
}
