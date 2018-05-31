package GoTinyDB

import (
	"bytes"
	"os"
	"testing"

	"github.com/alexandreStein/GoTinyDB/query"
	internalTesting "github.com/alexandreStein/GoTinyDB/testing"
	"github.com/alexandreStein/gods/utils"
)

var (
	rawExamples      = []internalTesting.TestValue{}
	defaultColName   = "col1"
	usernameSelector = []string{"UserName"}
)

// func TestIndex(t *testing.T) {
// 	defer os.RemoveAll(internalTesting.Path)
// 	db, openErr := Open(internalTesting.Path)
// 	if openErr != nil {
// 		t.Errorf("openning DB: %s", openErr.Error())
// 		return
// 	}
//
// 	col, openColErr := db.Use("col1")
// 	if openColErr != nil {
// 		t.Errorf("can't get the collection: %s", openColErr.Error())
// 		return
// 	}
// 	setIndexErr := col.SetIndex("test index", utils.StringComparatorType, []string{"UserName"})
// 	if setIndexErr != nil {
// 		t.Error(setIndexErr)
// 		return
// 	}
//
// 	for _, user := range internalTesting.GetUsersExample() {
// 		putErr := col.Put(user.GetID(), user)
// 		if putErr != nil {
// 			t.Errorf("puting the object: %s", putErr.Error())
// 			return
// 		}
// 	}
//
// 	// col.Query(q)
// }

func buildAndFeedDefaultDB(t *testing.T, path string) *DB {
	db, initErr := Open(path)
	if initErr != nil {
		t.Error(initErr.Error())
		return nil
	}

	col1, col1Err := db.Use(defaultColName)
	if col1Err != nil {
		t.Errorf("openning test collection: %s", col1Err.Error())
		return nil
	}

	if err := col1.SetIndex("UserName", utils.StringComparatorType, usernameSelector); err != nil {
		t.Errorf("setting index: %s", err.Error())
		return nil
	}

	for _, user := range internalTesting.GetUsersExample() {
		putErr := col1.Put(user.GetID(), user)
		if putErr != nil {
			t.Errorf("puting the object: %s", putErr.Error())
			return nil
		}
	}
	for _, raw := range rawExamples {
		putErr := col1.Put(raw.GetID(), raw.GetContent())
		if putErr != nil {
			t.Errorf("puting the object: %s", putErr.Error())
			return nil
		}
	}

	return db
}

func TestDB(t *testing.T) {
	defer os.RemoveAll(internalTesting.Path)
	rawExamples = internalTesting.GetRawExample()
	db := buildAndFeedDefaultDB(t, internalTesting.Path)
	defer db.Close()

	col1, getCollErr := db.Use(defaultColName)
	if getCollErr != nil {
		t.Errorf("getting the collection: %s", getCollErr.Error())
		return
	}

	for _, user := range internalTesting.GetUsersExample() {
		tmpUser := &internalTesting.UserTest{}
		queryObj := query.NewQuery()
		getAction := query.NewAction(query.Equal).SetSelector(usernameSelector)
		queryObj.Get(getAction).SetLimit(1)
		col1.Query(queryObj)
		getErr := col1.Get(user.GetID(), tmpUser)
		if getErr != nil {
			t.Errorf("getting the object: %s", getErr.Error())
			return
		}

		if !user.IsEqual(tmpUser) {
			t.Errorf("returned objects are not equal: \n%v\n%v", user, tmpUser)
			return
		}
	}
	for _, raw := range rawExamples {
		buf := bytes.NewBuffer(nil)
		getErr := col1.Get(raw.GetID(), buf)
		if getErr != nil {
			t.Errorf("getting record: %s", getErr.Error())
			return
		}

		if buf.String() != string(raw.GetContent().([]byte)) {
			t.Errorf("returned raw value is not the same as the given one")
			return
		}
	}

	for _, user := range internalTesting.GetUsersExample() {
		delUser := col1.Delete(user.GetID())
		if delUser != nil {
			t.Errorf("deleting the object: %s", delUser.Error())
			return
		}
	}
	for _, raw := range rawExamples {
		delErr := col1.Delete(raw.GetID())
		if delErr != nil {
			t.Errorf("deleting record: %s", delErr.Error())
			return
		}
	}

	for _, user := range internalTesting.GetUsersExample() {
		tmpUser := &internalTesting.UserTest{}
		getErr := col1.Get(user.GetID(), tmpUser)
		if getErr == nil {
			t.Errorf("the object has been deleted but is was found: %v", tmpUser)
			return
		}
	}
	for _, raw := range rawExamples {
		tmpRaw := bytes.NewBuffer(nil)
		delErr := col1.Get(raw.GetID(), tmpRaw)
		if delErr == nil {
			t.Errorf("raw value has been deleted bu found with length: %d", tmpRaw.Len())
			return
		}
	}
}

// func TestExistingDB(t *testing.T) {
// 	defer os.RemoveAll(internalTesting.Path)
// 	db, initErr := Open(internalTesting.Path)
// 	if initErr != nil {
// 		t.Error(initErr.Error())
// 		return
// 	}
// 	defer db.Close()
//
// 	col1, col1Err := db.Use("col1")
// 	if col1Err != nil {
// 		t.Errorf("openning test collection: %s", col1Err.Error())
// 		return
// 	}
//
// 	for _, user := range internalTesting.GetUsersExample() {
// 		tmpUser := &internalTesting.UserTest{}
// 		getErr := col1.Get(user.GetID(), tmpUser)
// 		if getErr != nil {
// 			t.Errorf("getting the object: %s", getErr.Error())
// 			return
// 		}
//
// 		if !user.IsEqual(tmpUser) {
// 			t.Errorf("returned objects are not equal: \n%v\n%v", user, tmpUser)
// 			return
// 		}
// 	}
// 	for _, raw := range rawExamples {
// 		buf := bytes.NewBuffer(nil)
// 		getErr := col1.Get(raw.GetID(), buf)
// 		if getErr != nil {
// 			t.Errorf("getting record: %s", getErr.Error())
// 			return
// 		}
//
// 		if buf.String() != string(raw.GetContent().([]byte)) {
// 			fmt.Printf("%x\n%x\n", buf.String(), string(raw.GetContent().([]byte)))
// 			t.Errorf("returned raw value is not the same as the given one")
// 			return
// 		}
// 	}
// }