package binding

import (
	"bytes"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/go-martini/martini"
)

func TestBind(t *testing.T) {
	testBind(t, false)
}

func TestBindWithInterface(t *testing.T) {
	testBind(t, true)
}

func TestMultipartBind(t *testing.T) {
	index := 0
	for test, expectStatus := range bindMultipartTests {
		handler := func(post BlogPost, errors Errors) {
			handle(test, t, index, post, errors)
		}
		recorder := testMultipart(t, test, Bind(BlogPost{}), handler, index)

		if recorder.Code != expectStatus {
			t.Errorf("On test case %+v, got status code %d but expected %d", test, recorder.Code, expectStatus)
		}

		index++
	}
}

func TestForm(t *testing.T) {
	testForm(t, false)
}

func TestFormWithInterface(t *testing.T) {
	testForm(t, true)
}

func TestEmptyForm(t *testing.T) {
	testEmptyForm(t)
}

func TestMultipartForm(t *testing.T) {
	for index, test := range multipartformTests {
		handler := func(post BlogPost, errors Errors) {
			handle(test, t, index, post, errors)
		}
		testMultipart(t, test, MultipartForm(BlogPost{}), handler, index)
	}
}

func TestMultipartFormWithInterface(t *testing.T) {
	for index, test := range multipartformTests {
		handler := func(post Modeler, errors Errors) {
			post.Create(test, t, index)
		}
		testMultipart(t, test, MultipartForm(BlogPost{}, (*Modeler)(nil)), handler, index)
	}
}

func TestMultipartFileForm(t *testing.T) {

	for idx, tc := range multipartformfileTests {
		req := buildFormFileReq(t, &tc)
		recorder := httptest.NewRecorder()
		handler := func(fup FileUpload, errors Errors) {
			handleFile(tc, t, &fup, errors, recorder, idx)
		}
		m := martini.Classic()
		m.Post(fileroute, MultipartForm(FileUpload{}), handler)
		m.ServeHTTP(recorder, req)
	}
}

func TestMultipartMultipleFileForm(t *testing.T) {
	for testIdx, tc := range multifileTests {
		req := buildFormFileReq(t, &tc)
		recorder := httptest.NewRecorder()
		handler := func(fup MultipleFileUpload, errors Errors) {
			// expecting everything to succeed
			if errors.Count() > 0 {
				t.Errorf("Expected no errors, got: %+v", errors)
			}

			assertEqualField(t, "Title", testIdx, tc.title, fup.Title)
			if len(tc.documents) != len(fup.Document) {
				t.Errorf("Expected %d documents, got: %+v", len(tc.documents), fup.Document)
			}

			for i, tcDocument := range tc.documents {
				if (fup.Document[i] == nil) != tcDocument.isNil {
					t.Errorf("Expected document.isNil: %+v, got %+v", tcDocument.isNil, fup.Document[i])
				}

				if fup.Document[i] != nil {
					assertEqualField(t, "Filename", testIdx, tcDocument.fileName, fup.Document[i].Filename)
					uploadData := unpackFileHeaderData(fup.Document[i], t)
					assertEqualField(t, "Document Data", testIdx, tcDocument.data, uploadData)
				}
			}
		}
		m := martini.Classic()
		m.Post(fileroute, MultipartForm(MultipleFileUpload{}), handler)
		m.ServeHTTP(recorder, req)
	}
}

func TestJson(t *testing.T) {
	testJson(t, false)
}

func TestJsonWithInterface(t *testing.T) {
	testJson(t, true)
}

func TestEmptyJson(t *testing.T) {
	testEmptyJson(t)
}

type missingJSON struct {
	Foo string `json:"foo" form:"bar" binding:"required"`
}

type missingForm struct {
	Foo string `form:"foo" binding:"required"`
}

func TestMissingRequiredTypeJSON(t *testing.T) {
	m := martini.Classic()
	m.Post("/", Bind(missingJSON{}), func() {})
	m.Put("/", Bind(missingJSON{}), func() {})
	for _, method := range []string{"POST", "PUT"} {
		req, _ := http.NewRequest(method, "/", bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		m.ServeHTTP(recorder, req)
		data, _ := ioutil.ReadAll(recorder.Body)
		if string(data) != `{"overall":{},"fields":{"foo":"Required"}}` {
			t.Error("Incorrect repsonse for missing required JSON field")
		}
	}
}

func TestMissingRequiredTypeForm(t *testing.T) {
	m := martini.Classic()
	m.Post("/", Bind(missingForm{}), func() {})
	req, _ := http.NewRequest("POST", "/", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, req)
	data, _ := ioutil.ReadAll(recorder.Body)
	if string(data) != `{"overall":{},"fields":{"foo":"Required"}}` {
		t.Error("Incorrect repsonse for missing required form field")
	}
}

func TestValidate(t *testing.T) {
	handlerMustErr := func(errors Errors) {
		if errors.Count() == 0 {
			t.Error("Expected at least one error, got 0")
		}
	}
	handlerNoErr := func(errors Errors) {
		if errors.Count() > 0 {
			t.Error("Expected no errors, got", errors.Count())
		}
	}

	performValidationTest(&BlogPost{"", "...", BlogPostMeta{0, 0, []int{}}}, handlerMustErr, t)
	performValidationTest(&BlogPost{"Good Title", "Good content", BlogPostMeta{0, 0, []int{}}}, handlerNoErr, t)

	performValidationTest(&User{Name: "Jim", Home: Address{"", ""}}, handlerMustErr, t)
	performValidationTest(&User{Name: "Jim", Home: Address{"required", ""}}, handlerNoErr, t)
}

func handle(test testCase, t *testing.T, index int, post BlogPost, errors Errors) {
	assertEqualField(t, "Title", index, test.ref.Title, post.Title)
	assertEqualField(t, "Content", index, test.ref.Content, post.Content)
	assertEqualField(t, "Views", index, test.ref.Views, post.Views)

	for i := range test.ref.Multiple {
		if i >= len(post.Multiple) {
			t.Errorf("Expected: %+v (size %d) to have same size as: %+v (size %d)", post.Multiple, len(post.Multiple), test.ref.Multiple, len(test.ref.Multiple))
			break
		}
		if test.ref.Multiple[i] != post.Multiple[i] {
			t.Errorf("Expected: %+v to deep equal: %+v", post.Multiple, test.ref.Multiple)
			break
		}
	}

	if test.ok && errors.Count() > 0 {
		t.Errorf("%+v should be OK (0 errors), but had errors: %+v", test, errors)
	} else if !test.ok && errors.Count() == 0 {
		t.Errorf("%+v should have errors, but was OK (0 errors)", test)
	}
}

func handleEmpty(test emptyPayloadTestCase, t *testing.T, index int, section BlogSection, errors Errors) {
	assertEqualField(t, "Title", index, test.ref.Title, section.Title)
	assertEqualField(t, "Content", index, test.ref.Content, section.Content)

	if test.ok && errors.Count() > 0 {
		t.Errorf("%+v should be OK (0 errors), but had errors: %+v", test, errors)
	} else if !test.ok && errors.Count() == 0 {
		t.Errorf("%+v should have errors, but was OK (0 errors): %+v", test)
	}
}

func handleFile(tc fileTestCase, t *testing.T, fup *FileUpload, errors Errors, recorder *httptest.ResponseRecorder, index int) {

	if (errors.Count() == 0) != tc.ok {
		t.Errorf("Expected tc.ok: %+v, got errors:%+v ", tc.ok, errors)
	}

	assertEqualField(t, "Status Code", index, tc.statusCode, recorder.Code)
	assertEqualField(t, "Title", index, tc.title, fup.Title)

	tcDocument := tc.documents[0]
	if (fup.Document == nil) != tcDocument.isNil {
		t.Errorf("Expected document.isNil: %+v, got %+v", tcDocument.isNil, fup.Document)
	}

	if fup.Document != nil {
		assertEqualField(t, "Filename", index, tcDocument.fileName, fup.Document.Filename)
		uploadData := unpackFileHeaderData(fup.Document, t)
		assertEqualField(t, "Document Data", index, tcDocument.data, uploadData)
	}
}

func unpackFileHeaderData(fh *multipart.FileHeader, t *testing.T) (data string) {
	if fh == nil {
		return
	}

	f, err := fh.Open()
	if err != nil {
		t.Error(err)
	}
	defer f.Close()

	var fb bytes.Buffer
	_, err = fb.ReadFrom(f)
	if err != nil {
		t.Error(err)
	}
	return fb.String()
}

func testBind(t *testing.T, withInterface bool) {
	index := 0
	for test, expectStatus := range bindTests {
		m := martini.Classic()
		recorder := httptest.NewRecorder()
		handler := func(post BlogPost, errors Errors) { handle(test, t, index, post, errors) }
		binding := Bind(BlogPost{})

		if withInterface {
			handler = func(post BlogPost, errors Errors) {
				post.Create(test, t, index)
			}
			binding = Bind(BlogPost{}, (*Modeler)(nil))
		}

		switch test.method {
		case "GET":
			m.Get(route, binding, handler)
		case "POST":
			m.Post(route, binding, handler)
		case "PUT":
			m.Put(route, binding, handler)
		case "DELETE":
			m.Delete(route, binding, handler)
		case "PATCH":
			m.Patch(route, binding, handler)
		}

		req, err := http.NewRequest(test.method, test.path, strings.NewReader(test.payload))
		req.Header.Add("Content-Type", test.contentType)

		if err != nil {
			t.Error(err)
		}
		m.ServeHTTP(recorder, req)

		if recorder.Code != expectStatus {
			t.Errorf("On test case %+v, got status code %d but expected %d", test, recorder.Code, expectStatus)
		}

		index++
	}
}

func testJson(t *testing.T, withInterface bool) {
	for index, test := range jsonTests {
		recorder := httptest.NewRecorder()
		handler := func(post BlogPost, errors Errors) { handle(test, t, index, post, errors) }
		binding := Json(BlogPost{})

		if withInterface {
			handler = func(post BlogPost, errors Errors) {
				post.Create(test, t, index)
			}
			binding = Bind(BlogPost{}, (*Modeler)(nil))
		}

		m := martini.Classic()
		switch test.method {
		case "GET":
			m.Get(route, binding, handler)
		case "POST":
			m.Post(route, binding, handler)
		case "PUT":
			m.Put(route, binding, handler)
		case "DELETE":
			m.Delete(route, binding, handler)
		}

		req, err := http.NewRequest(test.method, route, strings.NewReader(test.payload))
		if err != nil {
			t.Error(err)
		}
		m.ServeHTTP(recorder, req)
	}
}

func testEmptyJson(t *testing.T) {
	for index, test := range emptyPayloadTests {
		recorder := httptest.NewRecorder()
		handler := func(section BlogSection, errors Errors) { handleEmpty(test, t, index, section, errors) }
		binding := Json(BlogSection{})

		m := martini.Classic()
		switch test.method {
		case "GET":
			m.Get(route, binding, handler)
		case "POST":
			m.Post(route, binding, handler)
		case "PUT":
			m.Put(route, binding, handler)
		case "DELETE":
			m.Delete(route, binding, handler)
		}

		req, err := http.NewRequest(test.method, route, strings.NewReader(test.payload))
		if err != nil {
			t.Error(err)
		}
		m.ServeHTTP(recorder, req)
	}
}

func testForm(t *testing.T, withInterface bool) {
	for index, test := range formTests {
		recorder := httptest.NewRecorder()
		handler := func(post BlogPost, errors Errors) { handle(test, t, index, post, errors) }
		binding := Form(BlogPost{})

		if withInterface {
			handler = func(post BlogPost, errors Errors) {
				post.Create(test, t, index)
			}
			binding = Form(BlogPost{}, (*Modeler)(nil))
		}

		m := martini.Classic()
		switch test.method {
		case "GET":
			m.Get(route, binding, handler)
		case "POST":
			m.Post(route, binding, handler)
		}

		req, err := http.NewRequest(test.method, test.path, nil)
		if err != nil {
			t.Error(err)
		}
		m.ServeHTTP(recorder, req)
	}
}

func testEmptyForm(t *testing.T) {
	for index, test := range emptyPayloadTests {
		recorder := httptest.NewRecorder()
		handler := func(section BlogSection, errors Errors) { handleEmpty(test, t, index, section, errors) }
		binding := Form(BlogSection{})

		m := martini.Classic()
		switch test.method {
		case "GET":
			m.Get(route, binding, handler)
		case "POST":
			m.Post(route, binding, handler)
		}

		req, err := http.NewRequest(test.method, test.path, nil)
		if err != nil {
			t.Error(err)
		}
		m.ServeHTTP(recorder, req)
	}
}

func testMultipart(t *testing.T, test testCase, middleware martini.Handler, handler martini.Handler, index int) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()

	m := martini.Classic()
	m.Post(route, middleware, handler)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("title", test.ref.Title)
	writer.WriteField("content", test.ref.Content)
	writer.WriteField("views", strconv.Itoa(test.ref.Views))
	if len(test.ref.Multiple) != 0 {
		for _, value := range test.ref.Multiple {
			writer.WriteField("multiple", strconv.Itoa(value))
		}
	}

	req, err := http.NewRequest(test.method, test.path, body)
	req.Header.Add("Content-Type", writer.FormDataContentType())

	if err != nil {
		t.Error(err)
	}

	err = writer.Close()
	if err != nil {
		t.Error(err)
	}

	m.ServeHTTP(recorder, req)

	return recorder
}

func buildFormFileReq(t *testing.T, tc *fileTestCase) *http.Request {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("title", tc.title)
	for _, doc := range tc.documents {
		fw, err := w.CreateFormFile("document", doc.fileName)
		if err != nil {
			t.Error(err)
		}
		fw.Write([]byte(doc.data))
	}

	err := w.Close()
	if err != nil {
		t.Error(err)
	}

	req, err := http.NewRequest("POST", filepath, &b)
	if err != nil {
		t.Error(err)
	}

	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func assertEqualField(t *testing.T, fieldname string, testcasenumber int, expected interface{}, got interface{}) {
	if expected != got {
		t.Errorf("%s: expected=%s, got=%s in test case %d\n", fieldname, expected, got, testcasenumber)
	}
}

func performValidationTest(data interface{}, handler func(Errors), t *testing.T) {
	recorder := httptest.NewRecorder()
	m := martini.Classic()
	m.Get(route, Validate(data), handler)

	req, err := http.NewRequest("GET", route, nil)
	if err != nil {
		t.Error("HTTP error:", err)
	}

	m.ServeHTTP(recorder, req)
}

func (self BlogPost) Validate(errors *Errors, req *http.Request) {
	if len(self.Title) < 4 {
		errors.Fields["Title"] = "Too short; minimum 4 characters"
	}
	if len(self.Content) > 1024 {
		errors.Fields["Content"] = "Too long; maximum 1024 characters"
	}
	if len(self.Content) < 5 {
		errors.Fields["Content"] = "Too short; minimum 5 characters"
	}
}

func (self BlogPost) Create(test testCase, t *testing.T, index int) {
	assertEqualField(t, "Title", index, test.ref.Title, self.Title)
	assertEqualField(t, "Content", index, test.ref.Content, self.Content)
	assertEqualField(t, "Views", index, test.ref.Views, self.Views)

	for i := range test.ref.Multiple {
		if i >= len(self.Multiple) {
			t.Errorf("Expected: %v (size %d) to have same size as: %v (size %d)", self.Multiple, len(self.Multiple), test.ref.Multiple, len(test.ref.Multiple))
			break
		}
		if test.ref.Multiple[i] != self.Multiple[i] {
			t.Errorf("Expected: %v to deep equal: %v", self.Multiple, test.ref.Multiple)
			break
		}
	}
}

func (self BlogSection) Create(test emptyPayloadTestCase, t *testing.T, index int) {
	// intentionally left empty
}

type (
	testCase struct {
		method      string
		path        string
		payload     string
		contentType string
		ok          bool
		ref         *BlogPost
	}

	emptyPayloadTestCase struct {
		method      string
		path        string
		payload     string
		contentType string
		ok          bool
		ref         *BlogSection
	}

	fileTestCase struct {
		path       string
		title      string
		documents  []*fileInfo
		statusCode int
		ok         bool
	}

	fileInfo struct {
		isNil    bool
		data     string
		fileName string
	}

	Modeler interface {
		Create(test testCase, t *testing.T, index int)
	}

	BlogPostMeta struct {
		Views    int   `form:"views" json:"views"`
		internal int   `form:"-"`
		Multiple []int `form:"multiple"`
	}

	BlogPost struct {
		Title   string `form:"title" json:"title" binding:"required"`
		Content string `form:"content" json:"content"`
		BlogPostMeta
	}

	BlogSection struct {
		Title   string `form:"title" json:"title"`
		Content string `form:"content" json:"content"`
	}

	User struct {
		Name string  `json:"name" binding:"required"`
		Home Address `json:"address" binding:"required"`
	}

	Address struct {
		Street1 string `json:"street1" binding:"required"`
		Street2 string `json:"street2"`
	}

	FileUpload struct {
		Title    string                `form:"title" binding:"required"`
		Document *multipart.FileHeader `form:"document" binding:"required"`
	}

	MultipleFileUpload struct {
		Title    string                  `form:"title" binding:"required"`
		Document []*multipart.FileHeader `form:"document"`
	}
)

var (
	bindTests = map[testCase]int{
		// These should bail because of no Content-Type
		testCase{
			"POST",
			path,
			`no Content-Type POST"`,
			"",
			false,
			new(BlogPost),
		}: http.StatusUnsupportedMediaType,
		testCase{
			"PUT",
			path,
			`no Content-Type PUT"`,
			"",
			false,
			new(BlogPost),
		}: http.StatusUnsupportedMediaType,

		// These should bail at the deserialization/binding phase
		testCase{
			"POST",
			path,
			`{ bad JSON `,
			"application/json",
			false,
			new(BlogPost),
		}: http.StatusBadRequest,
		testCase{
			"POST",
			path,
			`not multipart but has content-type`,
			"multipart/form-data",
			false,
			new(BlogPost),
		}: http.StatusBadRequest,

		// These should deserialize, then bail at the validation phase
		testCase{
			"POST",
			path + "?title= This is wrong  ",
			`not URL-encoded but has content-type`,
			"x-www-form-urlencoded",
			false,
			new(BlogPost),
		}: StatusUnprocessableEntity, // according to comments in Form() -> although the request is not url encoded, ParseForm does not complain
		testCase{
			"GET",
			path + "?content=This+is+the+content",
			``,
			"x-www-form-urlencoded",
			false,
			&BlogPost{Title: "", Content: "This is the content"},
		}: StatusUnprocessableEntity,
		testCase{
			"GET",
			path + "",
			`{"content":"", "title":"Blog Post Title"}`,
			"application/json",
			false,
			&BlogPost{Title: "Blog Post Title", Content: ""},
		}: StatusUnprocessableEntity,

		// These should succeed
		testCase{
			"GET",
			path + "",
			`{"content":"This is the content", "title":"Blog Post Title"}`,
			"application/json",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		}: http.StatusOK,
		testCase{
			"GET",
			path + "?content=This+is+the+content&title=Blog+Post+Title",
			``,
			"", // no content-type
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		}: http.StatusOK,
		testCase{
			"GET",
			path + "?content=This is the content&title=Blog+Post+Title",
			`{"content":"This is the content", "title":"Blog Post Title"}`,
			"",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		}: http.StatusOK,
	}

	bindMultipartTests = map[testCase]int{
		// This should deserialize, then bail at the validation phase
		testCase{
			"POST",
			path,
			"",
			"multipart/form-data",
			false,
			&BlogPost{Title: "", Content: "This is the content"},
		}: StatusUnprocessableEntity,

		// This should succeed
		testCase{
			"POST",
			path,
			"",
			"multipart/form-data",
			true,
			&BlogPost{Title: "This is the Title", Content: "This is the content"},
		}: http.StatusOK,
	}

	formTests = []testCase{
		{
			"GET",
			path + "?content=This is the content",
			"",
			"",
			false,
			&BlogPost{Title: "", Content: "This is the content"},
		},
		{
			"POST",
			path + "?content=This+is+the+content&title=Blog+Post+Title&views=3",
			"",
			"",
			false, // false because POST requests should have a body, not just a query string
			&BlogPost{Title: "Blog Post Title", Content: "This is the content", BlogPostMeta: BlogPostMeta{Views: 3}},
		},
		{
			"GET",
			path + "?content=This+is+the+content&title=Blog+Post+Title&views=3&multiple=5&multiple=10&multiple=15&multiple=20",
			"",
			"",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content", BlogPostMeta: BlogPostMeta{Views: 3, Multiple: []int{5, 10, 15, 20}}},
		},
	}

	multipartformTests = []testCase{
		{
			"POST",
			path,
			"",
			"multipart/form-data",
			false,
			&BlogPost{Title: "", Content: "This is the content"},
		},
		{
			"POST",
			path,
			"",
			"multipart/form-data",
			false,
			&BlogPost{Title: "Blog Post Title", BlogPostMeta: BlogPostMeta{Views: 3}},
		},
		{
			"POST",
			path,
			"",
			"multipart/form-data",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content", BlogPostMeta: BlogPostMeta{Views: 3, Multiple: []int{5, 10, 15, 20}}},
		},
	}

	emptyPayloadTests = []emptyPayloadTestCase{
		{
			"GET",
			"",
			"",
			"",
			true,
			&BlogSection{},
		},
		{
			"POST",
			"",
			"",
			"",
			true,
			&BlogSection{},
		},
		{
			"PUT",
			"",
			"",
			"",
			true,
			&BlogSection{},
		},
		{
			"DELETE",
			"",
			"",
			"",
			true,
			&BlogSection{},
		},
	}
	multipartformfileTests = []fileTestCase{
		{
			path:  filepath,
			title: "Upload Please",
			documents: []*fileInfo{
				&fileInfo{
					false,
					"This is my body data.",
					"testdata.txt",
				},
			},
			statusCode: http.StatusOK,
			ok:         true,
		},
		{
			filepath,
			"My upload",
			[]*fileInfo{
				&fileInfo{
					true, // don't do a file upload
					"",
					"",
				},
			},
			http.StatusOK,
			false, // make sure we get an error (document is required)
		},
		{
			// form puts multiple documents; make sure we just get the first one when it gets to our binding
			filepath,
			"My upload multiple",
			[]*fileInfo{
				&fileInfo{
					false,
					"document1.txt",
					"I am the first document",
				},
				&fileInfo{
					false,
					"document2.txt",
					"I am the second document",
				},
			},
			http.StatusOK,
			true,
		},
	}

	multifileTests = []fileTestCase{
		{
			// form puts multiple documents. Expect this to work.
			path:  filepath,
			title: "My upload multiple",
			documents: []*fileInfo{
				&fileInfo{
					false,
					"document1.txt",
					"I am the first document",
				},
				&fileInfo{
					false,
					"document2.txt",
					"I am the second document",
				},
			},
			statusCode: http.StatusOK,
			ok:         true,
		},
	}

	jsonTests = []testCase{
		// bad requests
		{
			"GET",
			"",
			`{blah blah blah}`,
			"",
			false,
			&BlogPost{},
		},
		{
			"POST",
			"",
			`{asdf}`,
			"",
			false,
			&BlogPost{},
		},
		{
			"PUT",
			"",
			`{blah blah blah}`,
			"",
			false,
			&BlogPost{},
		},
		{
			"DELETE",
			"",
			`{;sdf _SDf- }`,
			"",
			false,
			&BlogPost{},
		},

		// Valid-JSON requests
		{
			"GET",
			"",
			`{"content":"This is the content"}`,
			"",
			false,
			&BlogPost{Title: "", Content: "This is the content"},
		},
		{
			"POST",
			"",
			`{}`,
			"application/json",
			false,
			&BlogPost{Title: "", Content: ""},
		},
		{
			"POST",
			"",
			`{"content":"This is the content", "title":"Blog Post Title"}`,
			"",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		},
		{
			"PUT",
			"",
			`{"content":"This is the content", "title":"Blog Post Title"}`,
			"",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		},
		{
			"DELETE",
			"",
			`{"content":"This is the content", "title":"Blog Post Title"}`,
			"",
			true,
			&BlogPost{Title: "Blog Post Title", Content: "This is the content"},
		},
	}
)

const (
	route     = "/blogposts/create"
	path      = "http://localhost:3000" + route
	fileroute = "/data"
	filepath  = "http://localhost:3000" + fileroute
)
