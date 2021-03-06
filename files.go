package gotinydb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/alexandrestein/gotinydb/cipher"
	"github.com/alexandrestein/gotinydb/transaction"
	"github.com/dgraph-io/badger"
	"golang.org/x/crypto/blake2b"
)

type (
	// FileStore defines database file storage object
	FileStore struct {
		db *DB
	}

	// FileMeta defines some file metadata informations
	FileMeta struct {
		ID                        string
		Name                      string
		Size                      int64
		LastModified              time.Time
		ChuckSize                 int
		RelatedDocumentID         string
		RelatedDocumentCollection string
		inWrite                   bool
	}

	readWriter struct {
		deadLineTimer   *time.Timer
		meta            *FileMeta
		cache           []byte
		cachedChunk     int
		fs              *FileStore
		currentPosition int64
		txn             *badger.Txn
		writer          bool
	}

	// Reader define a simple object to read parts of the file.
	// After 10 minutes (ReaderWriterTimeout variable) the reader
	// is automatically closed.
	Reader interface {
		io.ReadCloser
		io.Seeker
		io.ReaderAt

		GetMeta() *FileMeta
	}

	// Writer define a simple object to write parts of the file.
	// After 10 minutes (ReaderWriterTimeout variable) the writer
	// is automatically closed.
	Writer interface {
		Reader

		io.Writer
		io.WriterAt
	}
)

// PutFile let caller insert large element into the database via a reader interface
func (fs *FileStore) PutFile(id string, name string, reader io.Reader) (n int, err error) {
	return fs.PutFileRelated(id, name, reader, "", "")
}

// PutFileWithTTL let caller insert large element into the database via a reader interface
func (fs *FileStore) PutFileWithTTL(id string, name string, reader io.Reader, ttl time.Duration) (n int, err error) {
	// Add the new file
	n, err = fs.PutFileRelated(id, name, reader, "", "")
	go fs.putFileTTL(id, ttl)
	return n, err
}

func (fs *FileStore) putFileTTL(id string, ttl time.Duration) {
	ttlObj := newTTL("", id, true, ttl)

	ctx, cancel := context.WithTimeout(fs.db.ctx, time.Second*10)
	defer cancel()

	tx := transaction.New(ctx)
	tx.AddOperation(
		transaction.NewOperation(
			"",
			nil,
			ttlObj.timeAsKey(),
			ttlObj.exportAsBytes(),
			false,
			true,
		),
	)

	// Do the writing:
	select {
	case fs.db.writeChan <- tx:
	case <-fs.db.ctx.Done():
		return
	}

	// Wait for the response
	select {
	case <-tx.ResponseChan:
	case <-tx.Ctx.Done():
	}
}

// PutFileRelated does the same as *DB.PutFile but the file is automatically removed
// when the related document is removed.
// The use case can be for blog post for example. You have posts which has images and medias in it.
// Ones the post is removed the images and the medias are not needed anymore.
// This provide a easy way remove files automatically based on collection documents.
func (fs *FileStore) PutFileRelated(id string, name string, reader io.Reader, colName, documentID string) (n int, err error) {
	fs.DeleteFile(id)

	meta := fs.buildMeta(id, name)
	meta.inWrite = true

	if colName != "" {
		meta.RelatedDocumentCollection = colName
		meta.RelatedDocumentID = documentID

		// Save the related document
		err = fs.addRelatedFileIDs(colName, documentID, id)
		if err != nil {
			return
		}
	}

	// Set the meta
	err = fs.putFileMeta(meta)
	if err != nil {
		return
	}

	// Track the numbers of chunks
	nChunk := 1
	// Open a loop
	for true {
		// Initialize the read buffer
		buff := make([]byte, FileChuckSize)
		var nWritten int
		nWritten, err = reader.Read(buff)
		// The read is done and it returns
		if nWritten == 0 || err == io.EOF && nWritten == 0 {
			break
		}
		// Return error if any
		if err != nil && err != io.EOF {
			return
		}

		// Clean the buffer
		buff = buff[:nWritten]

		n = n + nWritten

		err = fs.writeFileChunk(id, nChunk, buff)
		if err != nil {
			return n, err
		}

		// Increment the chunk counter
		nChunk++
	}

	meta.Size = int64(n)
	meta.LastModified = time.Now()
	meta.inWrite = false
	err = fs.putFileMeta(meta)
	if err != nil {
		return
	}

	err = nil
	return
}

func (fs *FileStore) writeFileChunk(id string, chunk int, content []byte) (err error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if FileChuckSize < len(content) {
		return fmt.Errorf("the maximum chunk size is %d bytes long but the content to write is %d bytes long", FileChuckSize, len(content))
	}

	tx := transaction.New(ctx)
	tx.AddOperation(
		transaction.NewOperation("", nil, fs.buildFilePrefix(id, chunk), content, false, true),
	)
	// Run the insertion
	select {
	case fs.db.writeChan <- tx:
	case <-fs.db.ctx.Done():
		return fs.db.ctx.Err()
	}

	// And wait for the end of the insertion
	select {
	case err = <-tx.ResponseChan:
	case <-tx.Ctx.Done():
		err = tx.Ctx.Err()
	}
	return
}

func (fs *FileStore) getFileMetaWithTxn(txn *badger.Txn, id, name string) (meta *FileMeta, err error) {
	metaID := fs.buildFilePrefix(id, 0)

	var item *badger.Item
	item, err = txn.Get(metaID)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			meta = fs.buildMeta(id, name)
			return
		}
		return
	}

	var valAsEncryptedBytes []byte
	valAsEncryptedBytes, err = item.ValueCopy(valAsEncryptedBytes)
	if err != nil {
		return
	}

	var valAsBytes []byte
	valAsBytes, err = cipher.Decrypt(fs.db.privateKey, item.Key(), valAsEncryptedBytes)
	if err != nil {
		return
	}

	meta = new(FileMeta)
	err = json.Unmarshal(valAsBytes, meta)
	return meta, err
}

func (fs *FileStore) getFileMeta(id, name string) (meta *FileMeta, err error) {
	err = fs.db.badger.View(func(txn *badger.Txn) (err error) {
		meta, err = fs.getFileMetaWithTxn(txn, id, name)
		return
	})
	if err != nil {
		return
	}
	return
}

func (fs *FileStore) buildMeta(id, name string) (meta *FileMeta) {
	meta = new(FileMeta)
	meta.ID = id
	meta.Name = name
	meta.Size = 0
	meta.LastModified = time.Time{}
	meta.ChuckSize = FileChuckSize

	return
}

func (fs *FileStore) putFileMeta(meta *FileMeta) (err error) {
	metaID := fs.buildFilePrefix(meta.ID, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var metaAsBytes []byte
	metaAsBytes, err = json.Marshal(meta)
	if err != nil {
		return
	}

	tx := transaction.New(ctx)
	tx.AddOperation(
		transaction.NewOperation("", nil, metaID, metaAsBytes, false, true),
	)
	// Run the insertion
	select {
	case fs.db.writeChan <- tx:
	case <-fs.db.ctx.Done():
		return fs.db.ctx.Err()
	}
	// And wait for the end of the insertion
	select {
	case err = <-tx.ResponseChan:
	case <-tx.Ctx.Done():
		err = tx.Ctx.Err()
	}
	return
}

// buildRelatedFileID returns the id of the saved list of files related to the given document into the given collection
func (fs *FileStore) buildRelatedID(colName, documentID string) []byte {
	col, err := fs.db.Use(colName)
	if err != nil {
		return nil
	}
	id := []byte{prefixFilesRelated}
	id = append(id, col.prefix...)
	id = append(id, []byte(documentID)...)

	return id
}

func (fs *FileStore) getRelatedFileIDsInternal(colName, documentID string, txn *badger.Txn) (fileIDs []string, _ error) {
	relatedID := fs.buildRelatedID(colName, documentID)
	item, err := txn.Get(relatedID)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return []string{}, nil
		}
		return nil, err
	}

	valAsEncryptedBytes := []byte{}
	valAsEncryptedBytes, err = item.ValueCopy(valAsEncryptedBytes)
	if err != nil {
		return nil, err
	}

	var valAsBytes []byte
	valAsBytes, err = cipher.Decrypt(fs.db.privateKey, item.Key(), valAsEncryptedBytes)
	if err != nil {
		return nil, err
	}

	fileIDs = []string{}
	err = json.Unmarshal(valAsBytes, &fileIDs)
	return fileIDs, err
}

func (fs *FileStore) getRelatedFileIDs(colName, documentID string) (fileIDs []string) {
	fs.db.badger.View(func(txn *badger.Txn) (err error) {
		fileIDs, err = fs.getRelatedFileIDsInternal(colName, documentID, txn)
		return err
	})
	return
}

func (fs *FileStore) addRelatedFileIDs(colName, documentID string, fileIDsToAdd ...string) (err error) {
	return fs.db.badger.View(func(txn *badger.Txn) error {
		fileIDs, err := fs.getRelatedFileIDsInternal(colName, documentID, txn)
		if err != nil {
			return err
		}

		fileIDs = append(fileIDs, fileIDsToAdd...)

		var retBytes []byte
		retBytes, err = json.Marshal(fileIDs)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// And add it to the list of store IDs to delete
		tx := transaction.New(ctx)
		tx.AddOperation(
			transaction.NewOperation("", fileIDs, fs.buildRelatedID(colName, documentID), retBytes, false, true),
		)

		// Send the write request
		fs.db.writeChan <- tx

		// Wait for the write response
		select {
		case err = <-tx.ResponseChan:
		case <-tx.Ctx.Done():
			err = tx.Ctx.Err()
		}

		return err
	})
}

func (fs *FileStore) deleteRelatedFileIDs(colName, documentID string, fileIDsToDelete ...string) (err error) {
	return fs.db.badger.View(func(txn *badger.Txn) error {
		fileIDs, err := fs.getRelatedFileIDsInternal(colName, documentID, txn)
		if err != nil {
			return err
		}

		for i := len(fileIDs) - 1; i >= 0; i-- {
			for _, idToDelete := range fileIDsToDelete {
				if idToDelete == fileIDs[i] {
					fileIDs = append(fileIDs[:i], fileIDs[i+1:]...)
				}
			}
		}

		var retBytes []byte
		if len(fileIDs) != 0 {
			retBytes, err = json.Marshal(fileIDs)
			if err != nil {
				return err
			}
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// And add it to the list of store IDs to delete
		tx := transaction.New(ctx)
		if len(fileIDs) != 0 {
			tx.AddOperation(
				transaction.NewOperation("", fileIDs, fs.buildRelatedID(colName, documentID), retBytes, false, true),
			)
		} else {
			tx.AddOperation(
				transaction.NewOperation("", nil, fs.buildRelatedID(colName, documentID), nil, true, true),
			)
		}

		// Send the write request
		fs.db.writeChan <- tx

		// Wait for the write response
		select {
		case err = <-tx.ResponseChan:
		case <-tx.Ctx.Done():
			err = tx.Ctx.Err()
		}
		if err != nil {
			return err
		}

		return nil
	})
}

// ReadFile write file content into the given writer
func (fs *FileStore) ReadFile(id string, writer io.Writer) error {
	return fs.db.badger.View(func(txn *badger.Txn) error {
		storeID := fs.buildFilePrefix(id, -1)

		opt := badger.DefaultIteratorOptions
		opt.PrefetchSize = 3
		opt.PrefetchValues = true

		it := txn.NewIterator(opt)
		defer it.Close()

		for it.Seek(fs.buildFilePrefix(id, 1)); it.ValidForPrefix(storeID); it.Next() {
			var err error
			var valAsEncryptedBytes []byte
			valAsEncryptedBytes, err = it.Item().ValueCopy(valAsEncryptedBytes)
			if err != nil {
				return err
			}

			var valAsBytes []byte
			valAsBytes, err = cipher.Decrypt(fs.db.privateKey, it.Item().Key(), valAsEncryptedBytes)
			if err != nil {
				return err
			}

			_, err = writer.Write(valAsBytes)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// GetFileReader returns a struct to provide simple reading partial of big files.
// The default position is at the begining of the file.
func (fs *FileStore) GetFileReader(id string) (Reader, error) {
	rw, err := fs.newReadWriter(id, "", false, 0)
	return Reader(rw), err
}

// GetFileWriter returns a struct to provide simple partial write of big files.
// The default position is at the end of the file.
func (fs *FileStore) GetFileWriter(id, name string) (Writer, error) {
	return fs.GetFileWriterRelated(id, name, "", "")
}

// GetFileWriterWithTTL does the same as GetFileWriter but it's
// automatically removed after the given duration
func (fs *FileStore) GetFileWriterWithTTL(id, name string, ttl time.Duration) (Writer, error) {
	w, err := fs.GetFileWriterRelated(id, name, "", "")
	go fs.putFileTTL(id, ttl)
	return w, err
}

// GetFileWriterRelated does the same as GetFileWriter but with related document
func (fs *FileStore) GetFileWriterRelated(id, name string, colName, documentID string) (Writer, error) {
	rw, err := fs.newReadWriter(id, name, true, 0)
	if err != nil {
		return nil, err
	}

	if rw.meta.inWrite {
		return nil, ErrFileInWrite
	}

	if colName != "" {
		rw.meta.RelatedDocumentCollection = colName
		rw.meta.RelatedDocumentID = documentID

		// Save the related document
		err = fs.addRelatedFileIDs(colName, documentID, id)
		if err != nil {
			return nil, err
		}
	}

	rw.meta.inWrite = true
	err = fs.putFileMeta(rw.meta)
	if err != nil {
		return nil, err
	}

	rw.currentPosition = rw.meta.Size
	return Writer(rw), err
}

// DeleteFile deletes every chunks of the given file ID
func (fs *FileStore) DeleteFile(id string) (err error) {
	listOfTx := []*transaction.Transaction{}

	// Open a read transaction to get every IDs
	return fs.db.badger.View(func(txn *badger.Txn) error {
		// Build the file prefix
		storeID := fs.buildFilePrefix(id, -1)

		// Defines the iterator options to get only IDs
		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false
		opt.AllVersions = true

		// Initialize the iterator
		it := txn.NewIterator(opt)
		defer it.Close()

		// ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ctx, cancel := context.WithCancel(fs.db.ctx)
		defer cancel()

		// Go the the first file chunk
		for it.Seek(storeID); it.ValidForPrefix(storeID); it.Next() {
			// Copy the store key
			var key []byte
			key = it.Item().KeyCopy(key)
			// And add it to the list of store IDs to delete
			tx := transaction.New(ctx)
			tx.AddOperation(
				transaction.NewOperation("", nil, key, nil, true, true),
			)
			listOfTx = append(listOfTx, tx)
			fs.db.writeChan <- tx
		}

		for _, tx := range listOfTx {
			select {
			case err = <-tx.ResponseChan:
			case <-tx.Ctx.Done():
				err = tx.Ctx.Err()
			}
			if err != nil {
				return err
			}
		}

		var meta *FileMeta
		meta, err = fs.getFileMetaWithTxn(txn, id, "")
		fs.deleteRelatedFileIDs(meta.RelatedDocumentCollection, meta.RelatedDocumentID, id)

		// Close the view transaction
		return nil
	})
}

func (fs *FileStore) buildFilePrefix(id string, chunkN int) []byte {
	// Derive the ID to make sure no file ID overlap the other.
	// Because the files are chunked it needs to have a stable prefix for reading
	// and deletation.
	derivedID := blake2b.Sum256([]byte(id))

	// Build the prefix
	prefixWithID := append([]byte{prefixFiles}, derivedID[:]...)

	// Initialize the chunk part of the ID
	chunkPart := []byte{}

	// If less than zero it for deletation and only the prefix is returned
	if chunkN < 0 {
		return prefixWithID
	}

	// If it's the first chunk
	if chunkN == 0 {
		chunkPart = append(chunkPart, 0)
	} else {
		// Lockup the numbers of full bytes for the chunk ID
		nbFull := chunkN / 256
		restFull := chunkN % 256

		for index := 0; index < nbFull; index++ {
			chunkPart = append(chunkPart, 255)
		}
		chunkPart = append(chunkPart, uint8(restFull))
	}

	// Return the ID for the given file and ID
	return append(prefixWithID, chunkPart...)
}

func (fs *FileStore) newReadWriter(id, name string, writer bool, timeOut time.Duration) (_ *readWriter, err error) {
	rw := new(readWriter)
	rw.writer = writer
	if timeOut == 0 {
		rw.deadLineTimer = time.AfterFunc(ReaderWriterTimeout, rw.mustClose)
	} else {
		rw.deadLineTimer = time.AfterFunc(timeOut, rw.mustClose)
	}

	rw.meta, err = fs.getFileMeta(id, name)
	if err != nil {
		if err == badger.ErrKeyNotFound && writer {
			//  not found but it's ok for writer
			err = nil
		} else {
			// otherways the error is returned
			return nil, err
		}
	}

	rw.fs = fs
	rw.txn = fs.db.badger.NewTransaction(false)

	return rw, nil
}

// GetFileIterator returns a file iterator which help to list existing files
func (fs *FileStore) GetFileIterator() *FileIterator {
	iterOptions := badger.DefaultIteratorOptions
	iterOptions.PrefetchValues = true
	iterOptions.PrefetchSize = 1

	txn := fs.db.badger.NewTransaction(false)
	badgerIter := txn.NewIterator(iterOptions)

	badgerIter.Seek([]byte{prefixFiles})

	return &FileIterator{
		baseIterator: &baseIterator{
			txn:        txn,
			badgerIter: badgerIter,
		},
		fs: fs,
	}
}

// Read implements the io.Reader interface
func (r *readWriter) Read(dest []byte) (n int, err error) {
	block, inside := r.getBlockAndInsidePosition(r.currentPosition)

	opt := badger.DefaultIteratorOptions
	opt.PrefetchSize = 3
	opt.PrefetchValues = true

	it := r.txn.NewIterator(opt)
	defer it.Close()

	buffer := bytes.NewBuffer(nil)
	first := true

	filePrefix := r.fs.buildFilePrefix(r.meta.ID, -1)
	for it.Seek(r.fs.buildFilePrefix(r.meta.ID, block)); it.ValidForPrefix(filePrefix); it.Next() {
		if it.Item().IsDeletedOrExpired() {
			break
		}

		// they are a variable which is used later but because of the cache we declare it here
		var err error
		var valAsEncryptedBytes []byte
		var valAsBytes []byte
		if block == r.cachedChunk && r.cache != nil && first {
			valAsBytes = make([]byte, len(r.cache))
			copy(valAsBytes, r.cache)
			goto useCache
		}

		valAsEncryptedBytes, err = it.Item().ValueCopy(valAsEncryptedBytes)
		if err != nil {
			return 0, err
		}

		valAsBytes, err = cipher.Decrypt(r.fs.db.privateKey, it.Item().Key(), valAsEncryptedBytes)
		if err != nil {
			return 0, err
		}

		// Save for caching
		r.cache = make([]byte, len(valAsBytes))
		copy(r.cache, valAsBytes)
		r.cachedChunk = block
	useCache:

		var toAdd []byte
		if first {
			toAdd = valAsBytes[inside:]
		} else {
			toAdd = valAsBytes
		}

		buffer.Write(toAdd)
		if buffer.Len() >= len(dest) {
			copy(dest, buffer.Bytes()[:len(dest)])
			r.currentPosition += int64(len(dest))
			return len(dest), nil
		}

		first = false
	}

	copy(dest, buffer.Bytes())

	r.currentPosition = 0

	return buffer.Len(), io.EOF
}

func (r *readWriter) checkReadWriteAt(off int64) error {
	if r.meta.Size <= off {
		return fmt.Errorf("the offset can not be equal or bigger than the file")
	}
	return nil
}

// ReadAt implements the io.ReaderAt interface
func (r *readWriter) ReadAt(p []byte, off int64) (n int, err error) {
	err = r.checkReadWriteAt(off)
	if err != nil {
		return 0, err
	}

	r.currentPosition = off
	return r.Read(p)
}

func (r *readWriter) getExistingBlock(blockN int) (ret []byte, err error) {
	chunkID := r.fs.buildFilePrefix(r.meta.ID, blockN)
	var item *badger.Item
	item, err = r.txn.Get(chunkID)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			return []byte{}, nil
		}
		return
	}

	var valAsEncryptedBytes []byte
	valAsEncryptedBytes, err = item.ValueCopy(valAsEncryptedBytes)
	if err != nil {
		return nil, err
	}

	return cipher.Decrypt(r.fs.db.privateKey, item.Key(), valAsEncryptedBytes)
}

func (r *readWriter) Write(p []byte) (n int, err error) {
	// Get a new transaction to be able to call write multiple times
	defer r.afterWrite(len(p))

	block, inside := r.getBlockAndInsidePosition(r.currentPosition)

	var valAsBytes []byte
	valAsBytes, err = r.getExistingBlock(block)
	if err != nil {
		return 0, err
	}

	freeToWriteInThisChunk := FileChuckSize - inside
	if freeToWriteInThisChunk > len(p) {
		toWrite := []byte{}
		if inside <= len(valAsBytes) {
			toWrite = valAsBytes[:inside]
		}
		toWrite = append(toWrite, p...)

		// If the new content don't completely overwrite the previous content
		if existingAfterNewWriteStartPosition := inside + len(p); existingAfterNewWriteStartPosition < len(valAsBytes) {
			toWrite = append(toWrite, valAsBytes[existingAfterNewWriteStartPosition:]...)
		}

		return len(p), r.fs.writeFileChunk(r.meta.ID, block, toWrite)
	}

	toWriteInTheFirstChunk := valAsBytes[:inside]
	toWriteInTheFirstChunk = append(toWriteInTheFirstChunk, p[n:freeToWriteInThisChunk]...)
	err = r.fs.writeFileChunk(r.meta.ID, block, toWriteInTheFirstChunk)
	if err != nil {
		return n, err
	}

	n += freeToWriteInThisChunk
	block++

	done := false

newLoop:
	newEnd := n + FileChuckSize
	if newEnd > len(p) {
		newEnd = len(p)
		done = true
	}

	nextToWrite := p[n:newEnd]
	if done {
		valAsBytes, err = r.getExistingBlock(block)
		if err != nil {
			return 0, err
		}
		if len(valAsBytes) >= len(nextToWrite) {
			nextToWrite = append(nextToWrite, valAsBytes[len(nextToWrite):]...)
		}
	}

	err = r.fs.writeFileChunk(r.meta.ID, block, nextToWrite)
	if err != nil {
		return n, err
	}

	n += FileChuckSize
	block++

	if done {
		n = len(p)
		return
	}

	goto newLoop
}

func (r *readWriter) afterWrite(writtenLength int) {
	// Refresh the transaction
	r.txn.Discard()
	r.txn = r.fs.db.badger.NewTransaction(false)

	r.cachedChunk = 0

	r.meta.Size += int64(writtenLength)
	r.meta.LastModified = time.Now()

	r.currentPosition += int64(writtenLength)

	r.fs.putFileMeta(r.meta)
}

func (r *readWriter) getWrittenSize() (n int64) {
	opt := badger.DefaultIteratorOptions
	opt.PrefetchSize = 5
	opt.PrefetchValues = false

	it := r.txn.NewIterator(opt)
	defer it.Close()

	nbChunks := -1
	blockesPrefix := r.fs.buildFilePrefix(r.meta.ID, -1)
	var item *badger.Item

	var lastBlockItem *badger.Item
	for it.Seek(r.fs.buildFilePrefix(r.meta.ID, 1)); it.ValidForPrefix(blockesPrefix); it.Next() {
		item = it.Item()
		if item.IsDeletedOrExpired() {
			break
		}
		lastBlockItem = item
		nbChunks++
	}

	if lastBlockItem == nil {
		return 0
	}

	var encryptedValue []byte
	var err error
	encryptedValue, err = lastBlockItem.ValueCopy(encryptedValue)
	if err != nil {
		return
	}

	var valAsBytes []byte
	valAsBytes, err = cipher.Decrypt(r.fs.db.privateKey, item.Key(), encryptedValue)
	if err != nil {
		return
	}

	n = int64(nbChunks * r.meta.ChuckSize)
	n += int64(len(valAsBytes))

	return
}

func (r *readWriter) WriteAt(p []byte, off int64) (n int, err error) {
	err = r.checkReadWriteAt(off)
	if err != nil {
		return 0, err
	}

	r.currentPosition = off
	return r.Write(p)
}

// Seek implements the io.Seeker interface
func (r *readWriter) Seek(offset int64, whence int) (n int64, err error) {
	switch whence {
	case io.SeekStart:
		n = offset
	case io.SeekCurrent:
		n = r.currentPosition + offset
	case io.SeekEnd:
		n = r.meta.Size - offset
	default:
		err = fmt.Errorf("whence not recognized")
	}

	if n > r.meta.Size || n < 0 {
		err = fmt.Errorf("is out of the file")
	}

	r.currentPosition = n
	return
}

// Close should be called when done with the Reader
func (r *readWriter) Close() (err error) {
	if r.writer {
		r.meta.inWrite = false
		r.fs.putFileMeta(r.meta)
	}
	r.txn.Discard()
	r.deadLineTimer.Stop()
	return
}

func (r *readWriter) mustClose() {
	r.Close()
}

func (r *readWriter) GetMeta() *FileMeta {
	return r.meta
}

func (r *readWriter) getBlockAndInsidePosition(offset int64) (block, inside int) {
	return int(offset/int64(r.meta.ChuckSize)) + 1, int(offset) % r.meta.ChuckSize
}

// GetMeta returns the metadata of the actual cursor position
func (i *FileIterator) GetMeta() *FileMeta {
	return i.meta
}

// Next moves to the next valid metadata element
func (i *FileIterator) Next() error {
	i.badgerIter.Next()

goToNext:
	if !i.Valid() {
		return ErrFileItemIteratorNotValid
	}

	isMeta, err := i.isMetaChunk()
	if !isMeta || err != nil {
		if err != nil {
			return err
		}

		i.badgerIter.Next()
		goto goToNext
	}

	return nil
}

// Seek moves to the meta coresponding to the given id
func (i *FileIterator) Seek(id string) {
	i.badgerIter.Seek(i.fs.buildFilePrefix(id, 0))
}

// Valid checks if the cursor point a valid metadata document
func (i *FileIterator) Valid() bool {
	valid := i.valid([]byte{prefixFiles})
	if valid {
		i.isMetaChunk()
	}
	return valid
}

func (i *FileIterator) isMetaChunk() (bool, error) {
	dbKey := i.item.Key()
	if len(dbKey) != 34 || dbKey[len(dbKey)-1] != 0 {
		return false, nil
	}

	buff, err := i.decrypt()
	if err != nil {
		return false, err
	}

	meta := new(FileMeta)
	err = json.Unmarshal(buff, meta)
	if err != nil {
		return false, err
	}

	i.meta = meta

	return true, nil
}

func (i *FileIterator) decrypt() ([]byte, error) {
	valAsEncryptedBytes, err := i.item.ValueCopy(nil)
	if err != nil {
		return nil, err
	}

	var valAsBytes []byte
	valAsBytes, err = cipher.Decrypt(i.fs.db.privateKey, i.item.Key(), valAsEncryptedBytes)
	if err != nil {
		return nil, err
	}

	return valAsBytes, nil
}
