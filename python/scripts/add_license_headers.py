#!/usr/bin/env python3

import os
import sys
import stat
from pathlib import Path

LICENSE_HEADER = '''# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

'''

def add_license_header(file_path):
    try:
        # Get file permissions
        st = os.stat(file_path)
        mode = st.st_mode
        
        with open(file_path, 'r', encoding='utf-8') as f:
            content = f.read()
        
        # Skip if file already has license header
        if 'Licensed under the Apache License' in content:
            print(f"Skipping {file_path} - already has license header")
            return
        
        # Add license header
        with open(file_path, 'w', encoding='utf-8') as f:
            f.write(LICENSE_HEADER + content)
        
        # Restore file permissions
        os.chmod(file_path, mode)
        print(f"Added license header to {file_path}")
    except Exception as e:
        print(f"Error processing {file_path}: {str(e)}", file=sys.stderr)
        sys.exit(1)

def main():
    try:
        src_dir = Path(__file__).parent.parent / 'src'
        if not src_dir.exists():
            print(f"Error: {src_dir} does not exist", file=sys.stderr)
            sys.exit(1)
        
        for py_file in src_dir.rglob('*.py'):
            add_license_header(py_file)
    except Exception as e:
        print(f"Error: {str(e)}", file=sys.stderr)
        sys.exit(1)

if __name__ == '__main__':
    main() 