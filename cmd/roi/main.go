package main

import (
	"database/sql"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/securecookie"

	"github.com/studio2l/roi"
)

// dev는 현재 개발모드인지를 나타낸다.
var dev bool

// templates에는 사용자에게 보일 페이지의 템플릿이 담긴다.
var templates *template.Template

// hasThumbnail은 해당 특정 프로젝트 샷에 썸네일이 있는지 검사한다.
//
// 주의: 만일 썸네일 파일 검사시 에러가 나면 이 함수는 썸네일이 있다고 판단한다.
// 이 함수는 템플릿 안에서 쓰이기 때문에 프론트 엔드에서 한번 더 검사하게
// 만들기 위해서이다.
func hasThumbnail(prj, shot string) bool {
	_, err := os.Stat(fmt.Sprintf("roi-userdata/thumbnail/%s/%s.png", prj, shot))
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		return true // 함수 주석 참고
	}
	return true
}

// parseTemplate은 tmpl 디렉토리 안의 html파일들을 파싱하여 http 응답에 사용될 수 있도록 한다.
func parseTemplate() {
	templates = template.Must(template.New("").Funcs(template.FuncMap{
		"hasThumbnail": hasThumbnail,
	}).ParseGlob("tmpl/*.html"))
}

// executeTemplate은 템플릿과 정보를 이용하여 w에 응답한다.
// templates.ExecuteTemplate 대신 이 함수를 쓰는 이유는 개발모드일 때
// 재 컴파일 없이 업데이트된 템플릿을 사용할 수 있기 때문이다.
func executeTemplate(w http.ResponseWriter, name string, data interface{}) error {
	if dev {
		parseTemplate()
	}
	return templates.ExecuteTemplate(w, name, data)
}

// cookieHandler는 클라이언트 브라우저 세션에 암호화된 쿠키를 저장을 돕는다.
var cookieHandler = securecookie.New(
	securecookie.GenerateRandomKey(64),
	securecookie.GenerateRandomKey(32),
)

// setSession은 클라이언트 브라우저에 세션을 저장한다.
func setSession(w http.ResponseWriter, session map[string]string) error {
	encoded, err := cookieHandler.Encode("session", session)
	if err != nil {
		return err
	}
	c := &http.Cookie{
		Name:  "session",
		Value: encoded,
		Path:  "/",
	}
	http.SetCookie(w, c)
	return nil
}

// getSession은 클라이언트 브라우저에 저장되어 있던 세션을 불러온다.
func getSession(r *http.Request) (map[string]string, error) {
	c, _ := r.Cookie("session")
	if c == nil {
		return nil, nil
	}
	value := make(map[string]string)
	err := cookieHandler.Decode("session", c.Value, &value)
	if err != nil {
		return nil, err
	}
	return value, nil
}

// clearSession은 클라이언트 브라우저에 저장되어 있던 세션을 지운다.
func clearSession(w http.ResponseWriter) {
	c := &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	}
	http.SetCookie(w, c)
}

// rootHandler는 루트경로(/)를 포함해 정의되지 않은 페이지로의 사용자 접속을 처리한다.
func rootHandler(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
	}
	recipt := struct {
		LoggedInUser string
	}{
		LoggedInUser: session["userid"],
	}
	err = executeTemplate(w, "index.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

// loginHandler는 /login 페이지로 사용자가 접속했을때 로그인 페이지를 반환한다.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		id := r.Form.Get("id")
		if id == "" {
			http.Error(w, "id field emtpy", http.StatusBadRequest)
			return
		}
		pw := r.Form.Get("password")
		if pw == "" {
			http.Error(w, "password field emtpy", http.StatusBadRequest)
			return
		}
		db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		match, err := roi.UserHasPassword(db, id, pw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !match {
			http.Error(w, "entered password is not correct", http.StatusBadRequest)
			return
		}
		session := map[string]string{
			"userid": id,
		}
		err = setSession(w, session)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not set session: %s", err), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
	}
	recipt := struct {
		LoggedInUser string
	}{
		LoggedInUser: session["userid"],
	}
	err = executeTemplate(w, "login.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

// logoutHandler는 /logout 페이지로 사용자가 접속했을때 사용자를 로그아웃 시킨다.
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	clearSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// signupHandler는 /signup 페이지로 사용자가 접속했을때 가입 페이지를 반환한다.
func signupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		id := r.Form.Get("id")
		if id == "" {
			http.Error(w, "id field emtpy", http.StatusBadRequest)
			return
		}
		pw := r.Form.Get("password")
		if pw == "" {
			http.Error(w, "password field emtpy", http.StatusBadRequest)
			return
		}
		if len(pw) < 8 {
			http.Error(w, "password too short", http.StatusBadRequest)
			return
		}
		// 할일: password에 대한 컨펌은 프론트 엔드에서 하여야 함
		pwc := r.Form.Get("password_confirm")
		if pw != pwc {
			http.Error(w, "passwords are not matched", http.StatusBadRequest)
			return
		}
		db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
		if err != nil {
			fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = roi.AddUser(db, id, pw)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not add user: %s", err), http.StatusBadRequest)
			return
		}
		session := map[string]string{
			"userid": id,
		}
		err = setSession(w, session)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not set session: %s", err), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
	}
	recipt := struct {
		LoggedInUser string
	}{
		LoggedInUser: session["userid"],
	}
	err = executeTemplate(w, "signup.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

// profileHandler는 /profile 페이지로 사용자가 접속했을 때 사용자 프로필 페이지를 반환한다.
func profileHandler(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
		http.Redirect(w, r, "/login/", http.StatusSeeOther)
		return
	}
	db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Method == "POST" {
		r.ParseForm()
		u := roi.User{
			ID:          session["userid"],
			KorName:     r.Form.Get("kor-name"),
			Name:        r.Form.Get("name"),
			Team:        r.Form.Get("team"),
			Position:    r.Form.Get("position"),
			Email:       r.Form.Get("email"),
			PhoneNumber: r.Form.Get("phone-number"),
			EntryDate:   r.Form.Get("entry-date"),
		}
		err = roi.SetUser(db, session["userid"], u)
		if err != nil {
			http.Error(w, fmt.Sprintf("could not set user: %s", err), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/settings/profile", http.StatusSeeOther)
		return
	}
	u, err := roi.GetUser(db, session["userid"])
	if err != nil {
		http.Error(w, fmt.Sprintf("could not get user: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	fmt.Println(u)
	recipt := struct {
		LoggedInUser string
		User         roi.User
	}{
		LoggedInUser: session["userid"],
		User:         u,
	}
	err = executeTemplate(w, "profile.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

// updatePasswordHandler는 /update-password 페이지로 사용자가 패스워드 변경과 관련된 정보를 보내면
// 사용자 패스워드를 변경한다.
func updatePasswordHandler(w http.ResponseWriter, r *http.Request) {
	session, err := getSession(r)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not get session: %s", err), http.StatusInternalServerError)
		clearSession(w)
		return
	}
	r.ParseForm()
	oldpw := r.Form.Get("old-password")
	if oldpw == "" {
		http.Error(w, "old password field emtpy", http.StatusBadRequest)
		return
	}
	newpw := r.Form.Get("new-password")
	if newpw == "" {
		http.Error(w, "new password field emtpy", http.StatusBadRequest)
		return
	}
	if len(newpw) < 8 {
		http.Error(w, "new password too short", http.StatusBadRequest)
		return
	}
	// 할일: password에 대한 컨펌은 프론트 엔드에서 하여야 함
	newpwc := r.Form.Get("new-password-confirm")
	if newpw != newpwc {
		http.Error(w, "passwords are not matched", http.StatusBadRequest)
		return
	}
	db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	id := session["userid"]
	match, err := roi.UserHasPassword(db, id, oldpw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !match {
		http.Error(w, "entered password is not correct", http.StatusBadRequest)
		return
	}
	err = roi.SetUserPassword(db, id, newpw)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not change user password: %s", err), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/settings/profile", http.StatusSeeOther)
}

// searchHandler는 /search/ 하위 페이지로 사용자가 접속했을때 페이지를 반환한다.
func searchHandler(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Path[len("/search/"):]

	db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	prjRows, err := db.Query("SELECT code FROM projects")
	if err != nil {
		fmt.Fprintln(os.Stderr, "project selection error: ", err)
		return
	}
	defer prjRows.Close()
	prjs := make([]string, 0)
	for prjRows.Next() {
		prj := ""
		if err := prjRows.Scan(&prj); err != nil {
			fmt.Fprintln(os.Stderr, "error getting prject info from database: ", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		prjs = append(prjs, prj)
	}

	if code == "" && len(prjs) != 0 {
		// 할일: 추후 사용자가 마지막으로 선택했던 프로젝트로 이동
		http.Redirect(w, r, "/search/"+prjs[0], http.StatusSeeOther)
		return
	}
	found := false
	for _, p := range prjs {
		if p == code {
			found = true
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "not found project %s\n", code)
		return
		// http.Error(w, fmt.Sprintf("not found project: %s", code), http.StatusNotFound)
	}

	scenes, err := roi.SelectScenes(db, code)
	if err != nil {
		log.Fatal(err)
	}

	if err := r.ParseForm(); err != nil {
		log.Fatal(err)
	}
	sceneFilter := r.Form.Get("scene")
	shotFilter := r.Form.Get("shot")
	tagFilter := r.Form.Get("tag")
	statusFilter := r.Form.Get("status")
	shots, err := roi.SearchShots(db, code, sceneFilter, shotFilter, tagFilter, statusFilter)
	if err != nil {
		log.Fatal(err)
	}

	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
	}

	recipt := struct {
		LoggedInUser string
		Projects     []string
		Project      string
		Scenes       []string
		Shots        []roi.Shot
		FilterScene  string
		FilterShot   string
		FilterTag    string
		FilterStatus string
	}{
		LoggedInUser: session["userid"],
		Projects:     prjs,
		Project:      code,
		Scenes:       scenes,
		Shots:        shots,
		FilterScene:  sceneFilter,
		FilterShot:   shotFilter,
		FilterTag:    tagFilter,
		FilterStatus: statusFilter,
	}
	err = executeTemplate(w, "search.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

// shotHandler는 /shot/ 하위 페이지로 사용자가 접속했을때 샷 정보가 담긴 페이지를 반환한다.
func shotHandler(w http.ResponseWriter, r *http.Request) {
	pth := r.URL.Path[len("/shot/"):]
	pths := strings.Split(pth, "/")
	if len(pths) != 2 {
		http.NotFound(w, r)
		return
	}
	prj := pths[0]
	s := pths[1]
	db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error connecting to the database: ", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	shot, err := roi.FindShot(db, prj, s)
	if err != nil {
		log.Fatal(err)
	}
	if shot.Name == "" {
		http.NotFound(w, r)
		return
	}

	session, err := getSession(r)
	if err != nil {
		log.Print(fmt.Sprintf("could not get session: %s", err))
		clearSession(w)
	}

	recipt := struct {
		LoggedInUser string
		Project      string
		Shot         roi.Shot
	}{
		LoggedInUser: session["userid"],
		Project:      prj,
		Shot:         shot,
	}
	err = executeTemplate(w, "shot.html", recipt)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	dev = true

	var (
		https string
		cert  string
		key   string
	)
	flag.StringVar(&https, "https", ":443", "address to open https port. it doesn't offer http for security reason.")
	flag.StringVar(&cert, "cert", "cert/cert.pem", "https cert file. if you don't have one, use cert/generate-self-signed-cert.sh script.")
	flag.StringVar(&key, "key", "cert/key.pem", "https key file. if you don't have one, use cert/generate-self-signed-cert.sh script.")
	flag.Parse()

	db, err := sql.Open("postgres", "postgresql://maxroach@localhost:26257/roi?sslmode=disable")
	if err != nil {
		log.Fatal("error connecting to the database: ", err)
	}
	roi.CreateTableIfNotExists(db, "projects", roi.ProjectTableFields)
	roi.CreateTableIfNotExists(db, "users", roi.UserTableFields)

	parseTemplate()

	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandler)
	mux.HandleFunc("/login/", loginHandler)
	mux.HandleFunc("/logout/", logoutHandler)
	mux.HandleFunc("/settings/profile", profileHandler)
	mux.HandleFunc("/update-password", updatePasswordHandler)
	mux.HandleFunc("/signup/", signupHandler)
	mux.HandleFunc("/search/", searchHandler)
	mux.HandleFunc("/shot/", shotHandler)
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	thumbfs := http.FileServer(http.Dir("roi-userdata/thumbnail"))
	mux.Handle("/thumbnail/", http.StripPrefix("/thumbnail/", thumbfs))
	log.Fatal(http.ListenAndServeTLS(https, cert, key, mux))
}
