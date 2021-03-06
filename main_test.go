package gotinydb

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/blevesearch/bleve"
	"github.com/dgraph-io/badger"
)

var (
	testDB  *DB
	testCol *Collection

	testPath      = os.TempDir() + "/testDB"
	testConfigKey = [32]byte{}

	testColName = "collection name"

	testUserID      = "test ID"
	cloneTestUserID = "test ID clone"
	testUser        = &testUserStruct{
		"toto",
		"userName@internet.org",
		&Account{"Github", "https://www.github.com"},
	}
	cloneTestUser = &testUserStruct{
		"toto clone",
		"userName@internet.org",
		&Account{"Github", "https://www.github.com"},
	}

	testIndexName = "email"
	// testIndexSelector         = "Email"
	testIndexNameAccounts = "accounts"
	// testIndexSelectorAccounts = []string{"accounts", "Name"}
)

type (
	testUserStruct struct {
		Name  string   `json:"name"`
		Email string   `json:"email"`
		Oauth *Account `json:"oauth"`
	}
	Account struct {
		Name, URL string
	}
)

func (t *testUserStruct) Type() string {
	return "local.testUserStruct"
}

func init() {
	tmpKey, err := base64.RawStdEncoding.DecodeString("/HpNPL+GzfLDsA642L7jdKcLuaGV8ijv9f9prSGRGIg")
	if err != nil {
		log.Fatal(err)
	}

	copy(testConfigKey[:], tmpKey[:])

	os.RemoveAll(testPath)
}

func openT(t *testing.T) (err error) {
	testDB, err = Open(testPath, testConfigKey)
	if err != nil {
		t.Error(err)
		return err
	}

	testCol, err = testDB.Use(testColName)
	if err != nil {
		t.Error(err)
		return err
	}

	userDocumentMapping := bleve.NewDocumentStaticMapping()

	emailFieldMapping := bleve.NewTextFieldMapping()
	userDocumentMapping.AddFieldMappingsAt("email", emailFieldMapping)

	err = testCol.SetBleveIndex(testIndexName, userDocumentMapping)
	if err != nil {
		t.Error(err)
		return err
	}

	err = testCol.SetBleveIndex("all", bleve.NewDocumentMapping())
	if err != nil {
		t.Error(err)
		return err
	}

	err = testCol.Put(testUserID, testUser)
	if err != nil {
		t.Error(err)
		return err
	}
	err = testCol.Put(cloneTestUserID, cloneTestUser)
	if err != nil {
		t.Error(err)
		return err
	}

	return
}

func clean() {
	testDB.Close()
	os.RemoveAll(testPath)
}

func TestHistory(t *testing.T) {
	defer clean()
	openT(t)

	testID := "the history test ID"
	testCol.Put(testID, []byte("value 10"))
	testCol.Put(testID, []byte("value 9"))
	testCol.Put(testID, []byte("value 8"))
	testCol.Put(testID, []byte("value 7"))
	testCol.Put(testID, []byte("value 6"))
	testCol.Put(testID, []byte("value 5"))
	testCol.Put(testID, []byte("value 4"))
	testCol.Put(testID, []byte("value 3"))
	testCol.Put(testID, []byte("value 2"))
	testCol.Put(testID, []byte("value 1"))
	testCol.Put(testID, []byte("value 0"))

	// Load part of the history
	valuesAsBytes, err := testCol.History(testID, 5)
	if err != nil {
		t.Error(err)
		return
	}
	for i, val := range valuesAsBytes {
		if fmt.Sprintf("value %d", i) != string(val) {
			t.Errorf("the history is not what is expected")
			return
		}
	}

	// Load more than the existing history
	valuesAsBytes, err = testCol.History(testID, 15)
	if err != nil {
		t.Error(err)
		return
	}
	for i, val := range valuesAsBytes {
		if fmt.Sprintf("value %d", i) != string(val) {
			t.Errorf("the history is not what is expected")
			return
		}
	}

	// Update the value with a fresh history
	freshHistoryValue := []byte("brand new element")
	err = testCol.PutWithCleanHistory(testID, freshHistoryValue)
	if err != nil {
		t.Error(err)
		return
	}

	valuesAsBytes, err = testCol.History(testID, 5)
	if err != nil {
		t.Error(err)
		return
	}

	if l := len(valuesAsBytes); l > 1 {
		t.Errorf("history returned more than 1 value %d", l)
		return
	}
	if string(valuesAsBytes[0]) != string(freshHistoryValue) {
		t.Errorf("the returned content from history is not correct")
	}
}

func TestDeleteParts(t *testing.T) {
	defer clean()
	openT(t)

	bleveIndex, _ := testCol.GetBleveIndex(testIndexName)
	prefix := bleveIndex.prefix
	testCol.DeleteIndex(testIndexName)

	time.Sleep(time.Second)

	testDB.badger.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false
		iter := txn.NewIterator(opt)
		defer iter.Close()

		for iter.Seek(prefix); iter.ValidForPrefix(prefix); iter.Next() {
			t.Errorf("this id must be deleted %v", iter.Item().Key())
		}

		return nil
	})
	_, err := testCol.GetBleveIndex(testIndexName)
	if err == nil {
		t.Errorf("the index is deleted")
		return
	}

	prefix = testCol.prefix
	testDB.DeleteCollection(testColName)

	time.Sleep(time.Second)

	testDB.badger.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false
		iter := txn.NewIterator(opt)
		defer iter.Close()

		for iter.Seek(prefix); iter.ValidForPrefix(prefix); iter.Next() {
			t.Errorf("this id must be deleted %v", iter.Item().Key())
		}

		return nil
	})
}

func TestChangeKey(t *testing.T) {
	var key1, key2 [32]byte

	if tmpKey, err := base64.StdEncoding.DecodeString("uTMZvHtVB7JhQhQnokUpUdnJA3Gn/iDnzpLNYpJiKI4="); err != nil {
		t.Error(err)
		return
	} else {
		copy(key1[:], tmpKey)
	}
	if tmpKey, err := base64.StdEncoding.DecodeString("OPvAVDfcqZed3YHrKJZVr+2gxjV0mnalnLcT9rJRJHc="); err != nil {
		t.Error(err)
		return
	} else {
		copy(key2[:], tmpKey)
	}

	changeKeyDBPath := "./changeKeyDBPath"
	defer os.RemoveAll(changeKeyDBPath)

	testDB, err := Open(changeKeyDBPath, key1)
	if err != nil {
		t.Error(err)
		return
	}
	defer testDB.Close()

	col, _ := testDB.Use("test")
	err = col.Put("test", []byte("hello"))
	if err != nil {
		t.Error(err)
		return
	}

	err = testDB.UpdateKey(key2)
	if err != nil {
		t.Error(err)
		return
	}

	err = testDB.Close()
	if err != nil {
		t.Error(err)
		return
	}

	testDB2, err := Open(changeKeyDBPath, key2)
	if err != nil {
		t.Error(err)
		return
	}
	defer testDB2.Close()

	col, _ = testDB2.Use("test")
	savedContent, err := col.Get("test", nil)
	if err != nil {
		t.Error(err)
		return
	}

	if string(savedContent) != "hello" {
		t.Errorf("The returned value %q is not expected (%q)", string(savedContent), "hello")
		return
	}
}
