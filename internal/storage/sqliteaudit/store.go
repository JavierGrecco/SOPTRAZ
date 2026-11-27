package sqliteaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/JavierGrecco/SOPTRAZ/internal/audit"
	"github.com/JavierGrecco/SOPTRAZ/internal/contracts"
	_ "modernc.org/sqlite"
)
