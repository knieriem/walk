// Copyright 2010 The Walk Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package walk

import (
	"fmt"
	"syscall"
	"unsafe"
)

import . "github.com/lxn/go-winapi"

func withCompatibleDC(f func(hdc HDC) error) error {
	hdc := CreateCompatibleDC(0)
	if hdc == 0 {
		return newError("CreateCompatibleDC failed")
	}
	defer DeleteDC(hdc)

	return f(hdc)
}

func hPackedDIBFromHBITMAP(hBmp HBITMAP) (HGLOBAL, error) {
	var dib DIBSECTION
	if GetObject(HGDIOBJ(hBmp), unsafe.Sizeof(dib), unsafe.Pointer(&dib)) == 0 {
		return 0, newError("GetObject failed")
	}

	bmihSize := uintptr(unsafe.Sizeof(dib.DsBmih))
	pixelsSize := uintptr(
		int32(dib.DsBmih.BiBitCount) * dib.DsBmih.BiWidth * dib.DsBmih.BiHeight)

	totalSize := bmihSize + pixelsSize

	hPackedDIB := GlobalAlloc(GHND, totalSize)
	dest := GlobalLock(hPackedDIB)
	defer GlobalUnlock(hPackedDIB)

	src := unsafe.Pointer(&dib.DsBmih)

	MoveMemory(dest, src, bmihSize)

	dest = unsafe.Pointer(uintptr(dest) + bmihSize)
	src = unsafe.Pointer(uintptr(src) + bmihSize)

	MoveMemory(dest, src, pixelsSize)

	return hPackedDIB, nil
}

type Bitmap struct {
	hBmp       HBITMAP
	hPackedDIB HGLOBAL
	size       Size
}

func newBitmapFromHBITMAP(hBmp HBITMAP) (bmp *Bitmap, err error) {
	var dib DIBSECTION
	if GetObject(HGDIOBJ(hBmp), unsafe.Sizeof(dib), unsafe.Pointer(&dib)) == 0 {
		return nil, newError("GetObject failed")
	}

	bmih := &dib.DsBmih

	bmihSize := uintptr(unsafe.Sizeof(*bmih))
	pixelsSize := uintptr(int32(bmih.BiBitCount)*bmih.BiWidth*bmih.BiHeight) / 8

	totalSize := uintptr(bmihSize + pixelsSize)

	hPackedDIB := GlobalAlloc(GHND, totalSize)
	dest := GlobalLock(hPackedDIB)
	defer GlobalUnlock(hPackedDIB)

	src := unsafe.Pointer(&dib.DsBmih)

	MoveMemory(dest, src, bmihSize)

	dest = unsafe.Pointer(uintptr(dest) + bmihSize)
	src = dib.DsBm.BmBits

	MoveMemory(dest, src, pixelsSize)

	return &Bitmap{
		hBmp:       hBmp,
		hPackedDIB: hPackedDIB,
		size: Size{
			int(bmih.BiWidth),
			int(bmih.BiHeight),
		},
	}, nil
}

func NewBitmap(size Size) (bmp *Bitmap, err error) {
	var bmi BITMAPINFO
	hdr := &bmi.BmiHeader
	hdr.BiSize = uint32(unsafe.Sizeof(*hdr))
	hdr.BiBitCount = 24
	hdr.BiCompression = BI_RGB
	hdr.BiPlanes = 1
	hdr.BiWidth = int32(size.Width)
	hdr.BiHeight = int32(size.Height)

	err = withCompatibleDC(func(hdc HDC) error {
		hBmp := CreateDIBSection(hdc, &bmi, DIB_RGB_COLORS, nil, 0, 0)
		switch hBmp {
		case 0, ERROR_INVALID_PARAMETER:
			return newError("CreateDIBSection failed")
		}

		bmp, err = newBitmapFromHBITMAP(hBmp)
		return err
	})

	return
}

func NewBitmapFromFile(filePath string) (*Bitmap, error) {
	var gpBmp *GpBitmap
	if status := GdipCreateBitmapFromFile(syscall.StringToUTF16Ptr(filePath), &gpBmp); status != Ok {
		return nil, newError(fmt.Sprintf("GdipCreateBitmapFromFile failed with status '%s' for file '%s'", status, filePath))
	}
	defer GdipDisposeImage((*GpImage)(gpBmp))

	var hBmp HBITMAP
	if status := GdipCreateHBITMAPFromBitmap(gpBmp, &hBmp, 0); status != Ok {
		return nil, newError(fmt.Sprintf("GdipCreateHBITMAPFromBitmap failed with status '%s' for file '%s'", status, filePath))
	}

	return newBitmapFromHBITMAP(hBmp)
}

func (bmp *Bitmap) withSelectedIntoMemDC(f func(hdcMem HDC) error) error {
	return withCompatibleDC(func(hdcMem HDC) error {
		hBmpOld := SelectObject(hdcMem, HGDIOBJ(bmp.hBmp))
		if hBmpOld == 0 {
			return newError("SelectObject failed")
		}
		defer SelectObject(hdcMem, hBmpOld)

		return f(hdcMem)
	})
}

func (bmp *Bitmap) draw(hdc HDC, location Point) error {
	return bmp.withSelectedIntoMemDC(func(hdcMem HDC) error {
		size := bmp.Size()

		if !BitBlt(
			hdc,
			int32(location.X),
			int32(location.Y),
			int32(size.Width),
			int32(size.Height),
			hdcMem,
			0,
			0,
			SRCCOPY) {

			return lastError("BitBlt")
		}

		return nil
	})
}

func (bmp *Bitmap) drawStretched(hdc HDC, bounds Rectangle) error {
	return bmp.withSelectedIntoMemDC(func(hdcMem HDC) error {
		size := bmp.Size()

		if !StretchBlt(
			hdc,
			int32(bounds.X),
			int32(bounds.Y),
			int32(bounds.Width),
			int32(bounds.Height),
			hdcMem,
			0,
			0,
			int32(size.Width),
			int32(size.Height),
			SRCCOPY) {

			return newError("StretchBlt failed")
		}

		return nil
	})
}

func (bmp *Bitmap) handle() HBITMAP {
	return bmp.hBmp
}

func (bmp *Bitmap) Dispose() {
	if bmp.hBmp != 0 {
		DeleteObject(HGDIOBJ(bmp.hBmp))

		GlobalUnlock(bmp.hPackedDIB)
		GlobalFree(bmp.hPackedDIB)

		bmp.hPackedDIB = 0
		bmp.hBmp = 0
	}
}

func (bmp *Bitmap) Size() Size {
	return bmp.size
}
