//go:build windows

package sqlitetuf

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func validateTrustStoreParent(path string) error {
	volume := filepath.VolumeName(path)
	if volume == "" {
		return fmt.Errorf("no se pudo resolver el volumen del directorio padre")
	}
	root := volume + string(filepath.Separator)
	current := root
	if err := validateWindowsTrustDirectory(current); err != nil {
		return fmt.Errorf("validar raíz %q: %w", current, err)
	}

	relative := strings.TrimPrefix(filepath.Clean(path), root)
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return fmt.Errorf("la ruta de directorio contiene un componente padre")
		}
		current = filepath.Join(current, component)
		if err := validateWindowsTrustDirectory(current); err != nil {
			return fmt.Errorf("validar directorio ancestro %q: %w", current, err)
		}
	}
	return nil
}

func validateWindowsTrustDirectory(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return fmt.Errorf("debe ser un directorio real, no un reparse point")
	}
	return rejectBroadWritableDACL(handle, path)
}

func prepareTrustStoreFile(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return err
	}
	if info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return errTrustStorePathNotRegular
	}
	return rejectBroadWritableDACL(handle, path)
}

// rejectBroadWritableDACL rejects write-capable allow ACEs granted to broad
// built-in principals. It deliberately does not mutate inherited or user-managed
// ACLs; the caller can choose a more restrictive data directory when this fails.
func rejectBroadWritableDACL(handle windows.Handle, objectPath string) error {
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("leer DACL de %q: %w", objectPath, err)
	}
	if descriptor == nil {
		return fmt.Errorf("DACL no disponible para %q", objectPath)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		if err == nil {
			err = fmt.Errorf("DACL nula (acceso no restringido)")
		}
		return fmt.Errorf("DACL no verificable para %q: %w", objectPath, err)
	}

	broadSIDs := make([]*windows.SID, 0, 7)
	for _, sidType := range []windows.WELL_KNOWN_SID_TYPE{
		windows.WinWorldSid,
		windows.WinAuthenticatedUserSid,
		windows.WinBuiltinUsersSid,
		windows.WinBuiltinGuestsSid,
		windows.WinInteractiveSid,
		windows.WinNetworkSid,
		windows.WinAnonymousSid,
	} {
		sid, err := windows.CreateWellKnownSid(sidType)
		if err != nil {
			return fmt.Errorf("resolver principal amplio de DACL: %w", err)
		}
		broadSIDs = append(broadSIDs, sid)
	}

	const fileDeleteChild = 0x00000040
	writeMask := windows.ACCESS_MASK(
		windows.GENERIC_WRITE |
			windows.GENERIC_ALL |
			windows.DELETE |
			windows.WRITE_DAC |
			windows.WRITE_OWNER |
			windows.FILE_WRITE_DATA |
			windows.FILE_APPEND_DATA |
			windows.FILE_WRITE_EA |
			windows.FILE_WRITE_ATTRIBUTES |
			fileDeleteChild,
	)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("leer ACE %d de %q: %w", index, objectPath, err)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			// The basic allow ACE has the SID at a fixed offset. Extended/object/
			// callback ACE layouts are deliberately rejected rather than skipped.
		default:
			return fmt.Errorf("ACE de DACL tipo %d no compatible con la validación estricta de %q", ace.Header.AceType, objectPath)
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Mask&writeMask == 0 {
			continue
		}
		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		for _, broadSID := range broadSIDs {
			if aceSID.Equals(broadSID) {
				return fmt.Errorf("DACL de %q concede permisos de escritura a un grupo amplio", objectPath)
			}
		}
	}
	return nil
}
