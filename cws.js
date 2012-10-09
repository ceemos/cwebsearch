function safe_tags(str) {
    return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;') ;
}

var queryRunning = false
function doQuery(len) {
    queryRunning = true
    var q = $('input[name=q]').attr('value')
    $.post("/", {'q': q,'l': len, 't': len * 100}, function(c, status, xhr) {
        $('.content').empty()
        var patt = /^([^:]*):([0-9]*):(.*)$/mg
        var match
        while(match = patt.exec(c)) {
            $('.content').append(
                '<li><span class="file">' + safe_tags(match[1]) + '</span>' +
                    '<span class="line">' + safe_tags(match[2]) + '</span>' +
                    '<br><pre class="context">' + safe_tags(match[3]) + '</pre></li>')
            
        }
        queryRunning = false
    })
}

var timer = null

 $(function() {
    
    $("input[name=q]").keyup(function(e){
        $('.content').empty()
        if (!queryRunning) {
            doQuery(10)
        }
        
        window.clearTimeout(timer)
        timer = window.setTimeout(function(){doQuery(100)}, 1000)
    })

})
 
// kate: space-indent on; indent-width 4; mixedindent off; indent-mode cstyle; dynamic-word-wrap on; line-numbers on;