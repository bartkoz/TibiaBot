// The browser entry. Everything it does is name the environment the panel runs
// in; main_test.go asks for this path, so the file stays even though the panel
// itself moved into app.js.

import {createPanel} from './app.js';

createPanel(globalThis).start();
