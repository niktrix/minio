/*
 * Minio Cloud Storage, (C) 2016 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"errors"
	"hash"
	"io"

	"github.com/dchest/blake2b"
	"github.com/klauspost/reedsolomon"
)

// newHashWriters - inititialize a slice of hashes for the disk count.
func newHashWriters(diskCount int) []hash.Hash {
	hashWriters := make([]hash.Hash, diskCount)
	for index := range hashWriters {
		hashWriters[index] = newHash("blake2b")
	}
	return hashWriters
}

// newHash - gives you a newly allocated hash depending on the input algorithm.
func newHash(algo string) hash.Hash {
	switch algo {
	case "blake2b":
		return blake2b.New512()
	// Add new hashes here.
	default:
		// Default to blake2b.
		return blake2b.New512()
	}
}

// hashSum calculates the hash of the entire path and returns.
func hashSum(disk StorageAPI, volume, path string, writer hash.Hash) ([]byte, error) {
	// Allocate staging buffer of 128KiB for copyBuffer.
	buf := make([]byte, readSizeV1)

	// Copy entire buffer to writer.
	if err := copyBuffer(writer, disk, volume, path, buf); err != nil {
		return nil, err
	}

	// Return the final hash sum.
	return writer.Sum(nil), nil
}

// getDataBlockLen - get length of data blocks from encoded blocks.
func getDataBlockLen(enBlocks [][]byte, dataBlocks int) int {
	size := 0
	// Figure out the data block length.
	for _, block := range enBlocks[:dataBlocks] {
		size += len(block)
	}
	return size
}

// Writes all the data blocks from encoded blocks until requested
// outSize length. Provides a way to skip bytes until the offset.
func writeDataBlocks(dst io.Writer, enBlocks [][]byte, dataBlocks int, outOffset int64, outSize int64) (int64, error) {
	// Do we have enough blocks?
	if len(enBlocks) < dataBlocks {
		return 0, reedsolomon.ErrTooFewShards
	}

	// Do we have enough data?
	if int64(getDataBlockLen(enBlocks, dataBlocks)) < outSize {
		return 0, reedsolomon.ErrShortData
	}

	// Counter to decrement total left to write.
	write := outSize

	// Counter to increment total written.
	totalWritten := int64(0)

	// Write all data blocks to dst.
	for _, block := range enBlocks[:dataBlocks] {
		// Skip blocks until we have reached our offset.
		if outOffset >= int64(len(block)) {
			// Decrement offset.
			outOffset -= int64(len(block))
			continue
		} else {
			// Skip until offset.
			block = block[outOffset:]

			// Reset the offset for next iteration to read everything
			// from subsequent blocks.
			outOffset = 0
		}
		// We have written all the blocks, write the last remaining block.
		if write < int64(len(block)) {
			n, err := io.Copy(dst, bytes.NewReader(block[:write]))
			if err != nil {
				return 0, err
			}
			totalWritten += n
			break
		}
		// Copy the block.
		n, err := io.Copy(dst, bytes.NewReader(block))
		if err != nil {
			return 0, err
		}

		// Decrement output size.
		write -= n

		// Increment written.
		totalWritten += n
	}

	// Success.
	return totalWritten, nil
}

// getBlockInfo - find start/end block and bytes to skip for given offset, length and block size.
func getBlockInfo(offset, length, blockSize int64) (startBlock, endBlock, bytesToSkip int64) {
	// Calculate start block for given offset and how many bytes to skip to get the offset.
	startBlock = offset / blockSize
	bytesToSkip = offset % blockSize
	endBlock = length / blockSize
	return
}

// calculate the blockSize based on input length and total number of
// data blocks.
func getEncodedBlockLen(inputLen int64, dataBlocks int) (curEncBlockSize int64) {
	curEncBlockSize = (inputLen + int64(dataBlocks) - 1) / int64(dataBlocks)
	return curEncBlockSize
}

// copyN - copies from disk, volume, path to input writer until length
// is reached at volume, path or an error occurs. A success copyN returns
// err == nil, not err == EOF. Additionally offset can be provided to start
// the read at. copyN returns io.EOF if there aren't enough data to be read.
func copyN(writer io.Writer, disk StorageAPI, volume string, path string, offset int64, length int64) (err error) {
	// Use 128KiB staging buffer to read up to length.
	buf := make([]byte, readSizeV1)

	// Read into writer until length.
	for length > 0 {
		curLength := int64(readSizeV1)
		if length < readSizeV1 {
			curLength = length
		}
		nr, er := disk.ReadFile(volume, path, offset, buf[:curLength])
		if nr > 0 {
			nw, ew := writer.Write(buf[0:nr])
			if nw > 0 {
				// Decrement the length.
				length -= int64(nw)

				// Progress the offset.
				offset += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != int64(nw) {
				err = io.ErrShortWrite
				break
			}
		}
		if er == io.EOF || er == io.ErrUnexpectedEOF {
			break
		}
		if er != nil {
			err = er
			break
		}
	}

	// Success.
	return err
}

// copyBuffer - copies from disk, volume, path to input writer until either EOF
// is reached at volume, path or an error occurs. A success copyBuffer returns
// err == nil, not err == EOF. Because copyBuffer is defined to read from path
// until EOF. It does not treat an EOF from ReadFile an error to be reported.
// Additionally copyBuffer stages through the provided buffer; otherwise if it
// has zero length, returns error.
func copyBuffer(writer io.Writer, disk StorageAPI, volume string, path string, buf []byte) error {
	// Error condition of zero length buffer.
	if buf != nil && len(buf) == 0 {
		return errors.New("empty buffer in readBuffer")
	}

	// Starting offset for Reading the file.
	startOffset := int64(0)

	// Read until io.EOF.
	for {
		n, err := disk.ReadFile(volume, path, startOffset, buf)
		if n > 0 {
			var m int
			m, err = writer.Write(buf[:n])
			if err != nil {
				return err
			}
			if int64(m) != n {
				return io.ErrShortWrite
			}
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
		// Progress the offset.
		startOffset += n
	}

	// Success.
	return nil
}
