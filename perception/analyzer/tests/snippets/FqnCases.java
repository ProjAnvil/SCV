package com.example.app;

import com.example.gone.Missing;
import com.example.model.*;
import com.example.util.Helper;

// resolution matrix: superclass via wildcard import, one interface via
// explicit import, one via same-package, one via an import that resolves to
// no source type (Missing), one with no import at all (UnknownApi).
public class FqnCases extends BaseModel implements Helper, OtherThing, Missing, UnknownApi {
}
