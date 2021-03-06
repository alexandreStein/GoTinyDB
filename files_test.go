package gotinydb

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/dgraph-io/badger"
	"golang.org/x/crypto/blake2b"
)

func TestFiles(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		return
	}

	// Change the file size from 5MB to 100KB
	defaultFileChuckSize := FileChuckSize
	FileChuckSize = 100 * 1000
	defer func(defaultFileChuckSize int) {
		FileChuckSize = defaultFileChuckSize
	}(defaultFileChuckSize)

	// 100MB
	randBuff := make([]byte, 100*1000*1000)
	rand.Read(randBuff)

	fileID := "test file ID"
	n, err := testDB.GetFileStore().PutFile(fileID, "", bytes.NewBuffer(randBuff))
	if err != nil {
		t.Error(err)
		return
	}

	if n != len(randBuff) {
		t.Errorf("expected write size %d but had %d", len(randBuff), n)
		return
	}

	randHash := blake2b.Sum256(randBuff)

	readBuff := bytes.NewBuffer(nil)
	err = testDB.GetFileStore().ReadFile(fileID, readBuff)
	if err != nil {
		t.Error(err)
		return
	}

	readHash := blake2b.Sum256(readBuff.Bytes())

	if !reflect.DeepEqual(randHash, readHash) {
		t.Error("the saved file and the rand file are not equal")
		return
	}

	// Check the ids with chunk number are well generated
	err = testDB.badger.View(func(txn *badger.Txn) error {
		storeID := testDB.GetFileStore().buildFilePrefix(fileID, -1)

		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false

		it := txn.NewIterator(opt)
		defer it.Close()
		prevLastByte := -1
		for it.Seek(storeID); it.ValidForPrefix(storeID); it.Next() {
			lastByte := int(it.Item().Key()[len(it.Item().Key())-1:][0])
			if prevLastByte+1 != lastByte {
				if prevLastByte == 255 && lastByte != 0 {
					t.Errorf("generated incremental bytes is not good")
				}
			}
			prevLastByte = lastByte
		}

		return nil
	})
	if err != nil {
		t.Error(err)
		return
	}

	err = testDB.GetFileStore().DeleteFile(fileID)
	if err != nil {
		t.Error(err)
		return
	}

	err = testDB.badger.View(func(txn *badger.Txn) error {
		storeID := testDB.GetFileStore().buildFilePrefix(fileID, -1)

		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false

		it := txn.NewIterator(opt)
		defer it.Close()
		for it.Seek(storeID); it.ValidForPrefix(storeID); it.Next() {
			return fmt.Errorf("must be empty response")
		}

		return nil
	})
	if err != nil {
		t.Error(err)
		return
	}
}

func TestFilesMultipleWriteSameID(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		return
	}

	// ≊ 15MB
	randBuff := make([]byte, 15*999*1000)
	rand.Read(randBuff)

	fileID := "test file ID"

	n, err := testDB.GetFileStore().PutFile(fileID, "", bytes.NewBuffer(randBuff))
	if err != nil {
		t.Error(err)
		return
	}
	if n != len(randBuff) {
		t.Errorf("expected write size %d but had %d", len(randBuff), n)
		return
	}

	// New smaller file of ≊ 5MB
	randBuff = make([]byte, 5*999*1000)
	rand.Read(randBuff)

	n, err = testDB.GetFileStore().PutFile(fileID, "", bytes.NewBuffer(randBuff))
	if err != nil {
		t.Error(err)
		return
	}
	if n != len(randBuff) {
		t.Errorf("expected write size %d but had %d", len(randBuff), n)
		return
	}

	readBuff := bytes.NewBuffer(nil)
	err = testDB.GetFileStore().ReadFile(fileID, readBuff)
	if err != nil {
		t.Error(err)
		return
	}

	randHash := blake2b.Sum256(randBuff)
	readHash := blake2b.Sum256(readBuff.Bytes())

	if !reflect.DeepEqual(randHash, readHash) {
		t.Error("the saved file and the rand file are not equal")
		return
	}
}

func TestFilesReaderInterface(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		return
	}

	// ≊ 15MB
	randBuff := make([]byte, 15*999*1000)
	rand.Read(randBuff)

	fileID := "test file ID"

	n, err := testDB.GetFileStore().PutFile(fileID, "", bytes.NewBuffer(randBuff))
	if err != nil {
		t.Error(err)
		return
	}
	if n != len(randBuff) {
		t.Errorf("expected write size %d but had %d", len(randBuff), n)
		return
	}

	// Read into the middle
	interfaceReadAtTest(t, fileID, randBuff, 8484246, 500, 500)

	// Read to the end
	interfaceReadAtTest(t, fileID, randBuff, len(randBuff)-200, 500, 200)

	// Test seek
	reader, err := testDB.GetFileStore().GetFileReader(fileID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	reader.Seek(50, io.SeekStart)
	interfaceReadTestAfterSeek(t, reader, randBuff, 50, 100)
	reader.Seek(50, io.SeekCurrent)
	interfaceReadTestAfterSeek(t, reader, randBuff, 200, 100)
	reader.Seek(50, io.SeekEnd)
	interfaceReadTestAfterSeek(t, reader, randBuff, len(randBuff)-50, 50)
}

func interfaceReadAtTest(t *testing.T, fileID string, randBuff []byte, readStart, readLength, wantedN int) {
	reader, err := testDB.GetFileStore().GetFileReader(fileID)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	p := make([]byte, readLength)
	var n int
	n, err = reader.ReadAt(p, int64(readStart))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != wantedN {
		t.Fatalf("the number of reader bytes must be %d but had %d", wantedN, n)
	}

	randChunk := randBuff[readStart : readStart+wantedN]
	if !reflect.DeepEqual(randChunk, p[:wantedN]) {
		t.Fatal("the saved and retrived buffer must be equal but not")
	}
}

func interfaceReadTestAfterSeek(t *testing.T, reader Reader, randBuff []byte, readStart, wantedN int) {
	p := make([]byte, 100)
	n, err := reader.Read(p)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != wantedN {
		t.Fatalf("the number of reader bytes must be %d but had %d", wantedN, n)
	}

	randChunk := randBuff[readStart : readStart+wantedN]
	if !reflect.DeepEqual(randChunk, p[:wantedN]) {
		fmt.Println(randChunk, p)
		fmt.Println(readStart)
		t.Fatal("the saved and retrived buffer must be equal but not")
	}
}

func TestFilesWriterInterface(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		return
	}

	// ≊ 15MB
	randBuff := make([]byte, 15*999*1000)
	rand.Read(randBuff)

	fileID := "test file ID"

	n, err := testDB.GetFileStore().PutFile(fileID, "", bytes.NewBuffer(randBuff))
	if err != nil {
		t.Error(err)
		return
	}
	if n != len(randBuff) {
		t.Errorf("expected write size %d but had %d", len(randBuff), n)
		return
	}

	writer, err := testDB.GetFileStore().GetFileWriter(fileID, "")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	p := make([]byte, 200)
	n, err = writer.WriteAt(p, 500)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p) {
		t.Fatalf("written %d bytes but contain %d", n, len(p))
	}

	expected := randBuff[400:500]
	expected = append(expected, p...)
	expected = append(expected, randBuff[500+len(p):500+len(p)+100]...)
	testWriteFileParts(t, fileID, expected, 400)

	p = make([]byte, 7*999*1000)
	n, err = writer.WriteAt(p, 500)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(p) {
		t.Fatalf("written %d bytes but contain %d", n, len(p))
	}

	expected = randBuff[400:500]
	expected = append(expected, p...)
	expected = append(expected, randBuff[500+len(p):500+len(p)+100]...)
	testWriteFileParts(t, fileID, expected, 400)
}

func testWriteFileParts(t *testing.T, fileID string, expected []byte, at int64) {
	reader, err := testDB.GetFileStore().GetFileReader(fileID)
	if err != nil {
		t.Fatal(err)
	}

	p := make([]byte, len(expected))
	var n int
	n, err = reader.ReadAt(p, at)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, p) {
		t.Fatalf("the returned value is unexpected")
	}
	if n != len(expected) {
		t.Fatalf("the returned n is not corrected. Expected %d has %d", n, len(expected))
	}
}

func TestRelatedPutFiles(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		t.Error(err)
		return
	}

	// ≊ 15MB
	randBuff := make([]byte, 15*999*1000)
	rand.Read(randBuff)

	fileID := "test file ID"

	err = testCol.Put(fileID, struct{}{})
	if err != nil {
		t.Error(err)
		return
	}

	buff := bytes.NewBuffer(nil)
	_, err = testDB.GetFileStore().PutFileRelated(fileID, "", buff, testCol.Name(), fileID)
	if err != nil {
		t.Error(err)
		return
	}

	err = testCol.Delete(fileID)
	if err != nil {
		t.Error(err)
		return
	}

	buff.Reset()
	err = testDB.GetFileStore().ReadFile(fileID, buff)
	if err != nil {
		t.Error(err)
		return
	}

	if buff.Len() != 0 {
		t.Errorf("the file should be empty because the related document has been removed")
		return
	}
}

func TestRelatedFilesWriterInterface(t *testing.T) {
	defer clean()
	err := openT(t)
	if err != nil {
		t.Error(err)
		return
	}

	// ≊ 15MB
	randBuff := make([]byte, 15*999*1000)
	rand.Read(randBuff)

	fileID := "test file ID"

	err = testCol.Put(fileID, struct{}{})
	if err != nil {
		t.Error(err)
		return
	}

	var w Writer
	w, err = testDB.GetFileStore().GetFileWriterRelated(fileID, "", testCol.Name(), fileID)
	if err != nil {
		t.Error(err)
		return
	}
	defer w.Close()

	_, err = w.Write(randBuff)
	if err != nil {
		t.Error(err)
		return
	}

	w.Close()

	err = testCol.Delete(fileID)
	if err != nil {
		t.Error(err)
		return
	}

	buff := bytes.NewBuffer(nil)
	err = testDB.GetFileStore().ReadFile(fileID, buff)
	if err != nil {
		t.Error(err)
		return
	}

	if buff.Len() != 0 {
		t.Errorf("the file should be empty because the related document has been removed")
		return
	}
}

func TestWriteFileCopy(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
		return
	}

	defer clean()
	err := openT(t)
	if err != nil {
		t.Error(err)
		return
	}

	randBuff := make([]byte, 999*1000*67)
	rand.Read(randBuff)
	buff := bytes.NewBuffer(nil)
	var n int
	n, err = buff.Write(randBuff)
	if err != nil {
		t.Error(err)
		return
	}

	if n != len(randBuff) {
		t.Error("length are not equal", n, len(randBuff))
		return
	}

	var w Writer
	w, err = testDB.GetFileStore().GetFileWriter("test file copy", "no name")
	if err != nil {
		t.Error(err)
		return
	}
	defer w.Close()

	buff2 := make([]byte, 1000*1000*10)
	var written int64
	written, err = io.CopyBuffer(w, buff, buff2)
	if err != nil {
		t.Error(err)
		return
	}
	err = w.Close()
	if err != nil {
		t.Error(err)
		return
	}

	if int(written) != len(randBuff) {
		t.Error("length are not equal", written, len(randBuff))
		return
	}

	var r Reader
	r, err = testDB.GetFileStore().GetFileReader("test file copy")
	if err != nil {
		t.Error(err)
		return
	}
	defer r.Close()

	buffTarget := bytes.NewBuffer(nil)
	buff3 := make([]byte, 2048)
	var readed int64

	readed, err = io.CopyBuffer(buffTarget, r, buff3)
	if err != nil {
		t.Error(err)
		return
	}

	if int(readed) != len(randBuff) {
		t.Error("length are not equal", readed, len(randBuff))
		return
	}

	if !bytes.Equal(buffTarget.Bytes(), randBuff) {
		t.Error("The tow buffer are not equal")
		return
	}
}
