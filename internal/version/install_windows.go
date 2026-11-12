//go:build windows

package version

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func ownedByCurrentUser(path string, _ os.FileInfo) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("reading owner of %s: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil {
		return fmt.Errorf("refusing %s: file has no verifiable owner", path)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if !owner.Equals(user.User.Sid) {
		return fmt.Errorf("refusing %s: file is not owned by the current user", path)
	}
	return nil
}
