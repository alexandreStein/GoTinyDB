package simple

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/blevesearch/bleve"
)

var (
	db  *DB
	col *Collection

	path      = os.TempDir() + "/testDB"
	configKey = [32]byte{}

	colName = "collection name"

	testUserID = "test ID"
	testUser   = &user{"toto", "toto@internet.org"}

	indexName     = "email"
	indexSelector = "Email"
)

type (
	user struct {
		Name, Email string
	}
)

func init() {
	os.RemoveAll(path)
}

func TestMain(t *testing.T) {
	defer clean()
	err := open(t)
	if err != nil {
		return
	}

	retrievedUser := new(user)
	_, err = col.Get(testUserID, retrievedUser)
	if err != nil {
		t.Error(err)
		return
	}

	if !reflect.DeepEqual(retrievedUser, testUser) {
		t.Errorf("the users are not equal. Put %v and get %v", testUser, retrievedUser)
		return
	}

	fmt.Println(retrievedUser)

	query := bleve.NewQueryStringQuery(testUser.Email)
	searchRequest := bleve.NewSearchRequestOptions(query, 10, 0, true)
	var searchResult *SearchResult
	searchResult, err = col.Search(indexName, searchRequest)
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println("searchResult", searchResult)

	err = db.Close()
	if err != nil {
		t.Error(err)
		return
	}

	db = nil
	col = nil

	db, err = Open(path, configKey)
	if err != nil {
		t.Error(err)
		return
	}

	col, err = db.Use(colName)
	if err != nil {
		t.Error(err)
		return
	}

	query = bleve.NewQueryStringQuery(testUser.Email)
	searchRequest = bleve.NewSearchRequestOptions(query, 10, 0, true)
	searchResult, err = col.Search(indexName, searchRequest)
	if err != nil {
		t.Error(err)
		return
	}

	err = col.Delete(testUserID)
	if err != nil {
		t.Error(err)
		return
	}

	query = bleve.NewQueryStringQuery(testUser.Email)
	searchRequest = bleve.NewSearchRequestOptions(query, 10, 0, true)
	searchResult, err = col.Search(indexName, searchRequest)
	if err == nil {
		t.Errorf("the index should returns no result but had %s", searchResult.BleveSearchResult.String())
		return
	}
}

func open(t *testing.T) (err error) {
	db, err = Open(path, configKey)
	if err != nil {
		t.Error(err)
		return err
	}

	col, err = db.Use(colName)
	if err != nil {
		t.Error(err)
		return err
	}

	err = col.SetBleveIndex(indexName, bleve.NewIndexMapping(), indexSelector)
	if err != nil {
		t.Error(err)
		return err
	}

	err = col.Put(testUserID, testUser)
	if err != nil {
		t.Error(err)
		return err
	}

	return
}

func clean() {
	db.Close()
	os.RemoveAll(path)
}

func TestBackup(t *testing.T) {
	defer clean()
	err := open(t)
	if err != nil {
		return
	}

	var backup bytes.Buffer

	err = db.Backup(&backup)
	if err != nil {
		t.Error(err)
		return
	}

	restoredDBPath := os.TempDir() + "/restoredDB"
	defer os.RemoveAll(restoredDBPath)

	var restoredDB *DB
	restoredDB, err = Open(restoredDBPath, configKey)
	if err != nil {
		t.Error(err)
		return
	}

	err = restoredDB.Load(&backup)
	if err != nil {
		t.Error(err)
		return
	}

	var col2 *Collection
	col2, err = restoredDB.Use(colName)
	if err != nil {
		t.Error(err)
		return
	}

	query := bleve.NewQueryStringQuery(testUser.Email)
	searchRequest := bleve.NewSearchRequestOptions(query, 10, 0, true)
	var searchResult *SearchResult
	searchResult, err = col2.Search(indexName, searchRequest)
	if err != nil {
		t.Error(err)
		return
	}

	fmt.Println("searchResult", searchResult)
}
