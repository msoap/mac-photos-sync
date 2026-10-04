package photosdb

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
)

// Version is zero when the library does not provide a readable XML version plist.
func Version(root string) (int, error) {
	file, err := os.Open(filepath.Join(root, "database", "DataModelVersion.plist"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	dec := xml.NewDecoder(file)
	key := ""
	for {
		token, err := dec.Token()
		if err == io.EOF {
			return 0, nil
		}
		if err != nil {
			return 0, nil
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			if err = dec.DecodeElement(&key, &start); err != nil {
				return 0, err
			}
		case "integer":
			var version int
			if err = dec.DecodeElement(&version, &start); err != nil {
				return 0, err
			}
			if key == "LibrarySchemaVersion" {
				return version, nil
			}
		}
	}
}
