package com.sb.setter;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

// §5 setter injection.
@Component
class Helper {
}

@Component
class SetterConsumer {

    private Helper helper;

    @Autowired
    public void setHelper(Helper helper) {
        this.helper = helper;
    }
}
