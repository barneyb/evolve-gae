package evolvegae

import (
	"appengine"
	"appengine/datastore"
	"appengine/user"
	"encoding/json"
	"fmt"
	"github.com/barneyb/evolve"
	"github.com/gorilla/mux"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	DS_KIND_EVOLUTION = "Evolution"
	DS_KIND_USER      = "User"
)

type Evolution struct {
	key       *datastore.Key
	NextSeed  int64
	Latest    evolve.Genome
    Ancestry  []evolve.Genome `datastore:"-"`
	AncestryBytes  []byte `datastore:",noindex"`
    AncestorCount  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (e *Evolution) Load(c <-chan datastore.Property) error {
    if err := datastore.LoadStruct(e, c); err != nil {
        return err
    }
    e.Ancestry = make([]evolve.Genome, e.AncestorCount)
    err := json.Unmarshal(e.AncestryBytes, &e.Ancestry)
    if err != nil {
        panic(err)
    }
    return nil
}

func (e *Evolution) Save(c chan<- datastore.Property) error {
    e.AncestorCount = len(e.Ancestry)
    var err error
    e.AncestryBytes, err = json.Marshal(e.Ancestry)
    if err != nil {
        panic(err)
    }
    return datastore.SaveStruct(e, c)
}

type jsonEvolution struct {
	Id            string        `json:"id"`
	Survivor      evolve.Genome `json:"survivor"`
    Ancestry        []evolve.Genome `json:"ancestry"`
	AncestorCount int           `json:"ancestorCount"`
	CreatedAt     int64         `json:"created_at"`
	UpdatedAt     int64         `json:"updated_at"`
}

type User struct {
	key       *datastore.Key
	Email     string
	GoogleID  string
	CreatedAt time.Time
}

type req_createEvolution struct {
	Genome evolve.Genome
	Seed   int64
}

func init() {
	docs, err := ioutil.ReadFile("docs.html")
	if err != nil {
		panic(err)
	}
	r := mux.NewRouter()
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(docs)
	})
    r.HandleFunc("/-/auth", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Location", "/")
        w.WriteHeader(http.StatusFound)
        return
    })
	r.HandleFunc("/-/evolutions", addSlash).Methods("GET")
	r.HandleFunc("/-/evolutions/", getEvolutions).Methods("GET")
	r.HandleFunc("/-/evolutions/", createEvolution).Methods("POST")
	r.HandleFunc("/-/evolutions/{key}", getEvolution).Methods("GET")
	r.HandleFunc("/-/evolutions/{key}", deleteEvolution).Methods("DELETE")
	r.HandleFunc("/-/evolutions/{key}/", getEvolution).Methods("GET")
	r.HandleFunc("/-/evolutions/{key}/", deleteEvolution).Methods("DELETE")
	r.HandleFunc("/-/evolutions/{key}/evolve", getNextGeneration).Methods("GET")
	r.HandleFunc("/-/evolutions/{key}/evolve", createSurvivor).Methods("POST")
	http.Handle("/", r)
}

func addSlash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", fmt.Sprintf("%v/", r.URL))
	w.WriteHeader(http.StatusFound)
	return
}

func getEvolutions(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	user := getUser(c)
	es := listEvolutions(c, user)
	jes := make([]jsonEvolution, 0, len(es))
	for _, e := range es {
		jes = append(jes, *toJsonEvolution(&e))
	}
	sendAsJson(w, &jes)
}

func sendAsJson(w http.ResponseWriter, it interface{}) {
	w.Header().Set("Content-Type", "application/json")
	encr := json.NewEncoder(w)
	encr.Encode(it)
}

func toJsonEvolution(e *Evolution) *jsonEvolution {
	return &jsonEvolution{
		Id:            e.key.Encode(),
		Survivor:      e.Latest,
		AncestorCount: len(e.Ancestry),
		CreatedAt:     e.CreatedAt.UnixNano(),
		UpdatedAt:     e.UpdatedAt.UnixNano(),
	}
}

func listEvolutions(c appengine.Context, u *User) []Evolution {
	q := datastore.NewQuery(DS_KIND_EVOLUTION).Ancestor(u.key).Order("-UpdatedAt")
	ec, err := q.Count(c)
	if err != nil {
		panic(err)
	}
	es := make([]Evolution, 0, ec)
	ks, err := q.GetAll(c, &es)
	if err != nil {
		panic(err)
	}
	for i, l := 0, len(es); i < l; i++ {
		es[i].key = ks[i]
	}
	return es
}

func createEvolution(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var body req_createEvolution
	if err := decoder.Decode(&body); err != nil {
		panic(err)
	}
	if body.Seed == 0 {
		body.Seed = time.Now().UnixNano()
	}
	t := time.Now()
	e := Evolution{
		NextSeed:  body.Seed,
		Latest:    body.Genome,
		CreatedAt: t,
		UpdatedAt: t,
	}
	c := appengine.NewContext(r)
	key := saveEvolution(c, getUser(c), &e)
	http.Redirect(w, r, key.Encode(), http.StatusFound)
}

func saveEvolution(c appengine.Context, u *User, e *Evolution) *datastore.Key {
	var key *datastore.Key
	if e.key == nil {
		key = datastore.NewIncompleteKey(c, DS_KIND_EVOLUTION, u.key)
	} else {
		key = e.key
	}
	key, err := datastore.Put(c, key, e)
	if err != nil {
		panic(err)
	}
	e.key = key
	return key
}

func getEvolution(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key, err := datastore.DecodeKey(vars["key"])
	if err != nil {
		panic(err)
	}
	c := appengine.NewContext(r)
	e := readEvolution(c, key)
    je := toJsonEvolution(e)
    je.Ancestry = e.Ancestry
	sendAsJson(w, je)
}

func readEvolution(c appengine.Context, key *datastore.Key) *Evolution {
	var e Evolution
	err := datastore.Get(c, key, &e)
	if err != nil {
		panic(err)
	}
	e.key = key
	return &e
}

func deleteEvolution(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key, err := datastore.DecodeKey(vars["key"])
	if err != nil {
		panic(err)
	}
	c := appengine.NewContext(r)
	err = datastore.Delete(c, key)
	if err != nil {
		panic(err)
	}
}

func getNextGeneration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key, err := datastore.DecodeKey(vars["key"])
	if err != nil {
		panic(err)
	}
	ns := r.FormValue("n")
	var n int
	if ns == "" {
		n = 8
	} else {
		n, err = strconv.Atoi(ns)
		if err != nil {
			panic(err)
		}
	}
	c := appengine.NewContext(r)
	var is []evolve.Individual
	err = datastore.RunInTransaction(c, func(c appengine.Context) error {
		e := readEvolution(c, key)
		ee := evolve.NewRand(
			&e.Latest,
			development,
			rand.New(rand.NewSource(e.NextSeed)),
		)
		is = ee.Evolve(n)
		e.NextSeed = ee.Rand.Int63()
		e.UpdatedAt = time.Now()
		saveEvolution(c, getUser(c), e)
		return nil
	}, nil)
	if err != nil {
		panic(err)
	}
	gs := make([]evolve.Genome, 0, len(is))
	for _, ind := range is {
		gs = append(gs, *ind.Genotype)
	}
	sendAsJson(w, gs)
}

func development(g *evolve.Genome) *evolve.Individual {
	return &evolve.Individual{
		Genotype: g,
	}
}

func createSurvivor(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	var survivor evolve.Genome
	if err := decoder.Decode(&survivor); err != nil {
		panic(err)
	}
	vars := mux.Vars(r)
	key, err := datastore.DecodeKey(vars["key"])
	if err != nil {
		panic(err)
	}
	c := appengine.NewContext(r)
	err = datastore.RunInTransaction(c, func(c appengine.Context) error {
		e := readEvolution(c, key)
        e.Ancestry = append(e.Ancestry, e.Latest)
		e.Latest = survivor
		e.UpdatedAt = time.Now()
		saveEvolution(c, getUser(c), e)
		return nil
	}, nil)
	if err != nil {
		panic(err)
	}
}

func getUser(c appengine.Context) *User {
	u := user.Current(c)
	if u == nil {
		panic("no contextual user")
	}
	var stringId string
	if u.ID != "" {
		stringId = u.ID
	} else {
		stringId = u.Email
	}
	key := datastore.NewKey(c, DS_KIND_USER, stringId, 0, nil)
	var user User
	err := datastore.Get(c, key, &user)
	if err == datastore.ErrNoSuchEntity {
		user.Email = u.Email
		user.GoogleID = u.ID
		user.CreatedAt = time.Now()
		key, err = datastore.Put(c, key, &user)
	}
	if err != nil {
		panic(err)
	}
	user.key = key
	return &user
}
