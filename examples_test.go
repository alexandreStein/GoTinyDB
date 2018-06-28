package gotinydb

import (
	"encoding/json"

	"github.com/alexandrestein/gotinydb/vars"
)

type (
	Type struct{}
)

var (
	responseQuery *ResponseQuery
	result        = []*Type{}
)

func ExampleResponseQuery_All() {
	i := 0
	if _, err := responseQuery.All(func(id string, objAsBytes []byte) error {
		tmpObj := new(Type)
		err := json.Unmarshal(objAsBytes, tmpObj)
		if err != nil {
			return err
		}
		// Add the element into the slice
		result[i] = tmpObj

		i++
		return nil
	}); err != nil {
		// Do something
	}
}

func ExampleResponseQuery_Next() {
	// List all result from the first to the last with the next function
	for i, _, v := responseQuery.First(); i >= 0; i, _, v = responseQuery.Next() {
		tmpObj := new(Type)
		err := json.Unmarshal(v, tmpObj)
		if err != nil {
			// Do something
		}

		result[i] = tmpObj
	}
	// List is fulled up
}
func ExampleResponseQuery_First() {
	// List all result from the first to the last with the next function
	for i, _, v := responseQuery.First(); i >= 0; i, _, v = responseQuery.Next() {
		tmpObj := new(Type)
		err := json.Unmarshal(v, tmpObj)
		if err != nil {
			// Do something
		}

		result[i] = tmpObj
	}
	// List is fulled up
}

func ExampleResponseQuery_Prev() {
	// List all result from the last to the last with the prev function
	for i, _, v := responseQuery.Last(); i >= 0; i, _, v = responseQuery.Prev() {
		tmpObj := new(Type)
		err := json.Unmarshal(v, tmpObj)
		if err != nil {
			// Do something
		}

		result[i] = tmpObj
	}
	// List is fulled up
}
func ExampleResponseQuery_Last() {
	// List all result from the last to the last with the prev function
	for i, _, v := responseQuery.Last(); i >= 0; i, _, v = responseQuery.Prev() {
		tmpObj := new(Type)
		err := json.Unmarshal(v, tmpObj)
		if err != nil {
			// Do something
		}

		result[i] = tmpObj
	}
	// List is fulled up
}

func ExampleResponseQuery_One() {
	for i := 0; true; i++ {
		tmpObj := new(Type)
		_, err := responseQuery.One(tmpObj)
		if err != nil {
			if err == vars.ErrTheResponseIsOver {
				break
			}
			// Do something
		}
		result[i] = tmpObj
	}
	// List is fulled up
}
