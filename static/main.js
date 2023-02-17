
var explorer = {
    init:function(divID,fileFn,newFileInfo){
        explorer.divID=divID;
        this.fileFn=fileFn;
        this.newFileInfo=newFileInfo;
    },
    divID:"",
    fileFn:null,
    newFileInfo:null,
    listfile:function (p){
        $.get("/explorer?p="+encodeURIComponent(p),function (d,status,xhr){
            if(d.code==200){
                $(explorer.divID).text("");

                $(explorer.divID).append('<input id="basePath" type="text" hidden value="'+d.path+'" />');

                if(d.allPartitions!=null){
                    var eles = d.allPartitions.map(function(part){
                        return `💾<a href="#" style="margin-right: 10px;" onclick="explorer.listfile(this.text)" >`+part+'\\</a>';
                    }).join('');
                    $(explorer.divID).append(eles);
                }
                //
                //
                $(explorer.divID).append('<br/><br/>');
                var sp="/"
                if(d.allPartitions!=null){
                    sp="\\"
                }
                var pathItems = d.path.split(sp)
                var aitem=''
                var apath=''
                for(var i=0;i<pathItems.length;i++){
                    aitem = pathItems[i]
                    if(i==0 ){
                        apath=aitem
                    }else{
                        if(d.allPartitions!=null){
                            apath=apath+sp+sp+aitem
                        }else{
                            apath=apath+sp+aitem
                        }
                    }
                    if(i==0 ){
                        if(d.allPartitions!=null){
                            $(explorer.divID).append(`<a href="#" style="font-size: 1.17em;font-weight: bold;" onclick='explorer.listfile("`+apath+'\\\\'+`")'' >`+aitem+'</a>');
                        }else{
                            $(explorer.divID).append(`<a href="#" style="font-size: 1.17em;font-weight: bold;" onclick='explorer.listfile("/")'' >`+'/</a>');
                        }
                    }else{
                        $(explorer.divID).append(sp);
                        if(i==(pathItems.length-1)){
                            $(explorer.divID).append(`<span style="font-size: 1.17em;font-weight: bold;" >`+aitem+'</span>');
                        }else{
                            $(explorer.divID).append(`<a href="#" style="font-size: 1.17em;font-weight: bold;" onclick='explorer.listfile("`+apath+`")'' >`+aitem+'</a>');
                        }
                    }
                }
                $(explorer.divID).append('<br/><br/>');
                //
                //
                var eles = d.items.map(function(item){
                    var ret=''
                    if(item.IsDir){
                        ret=`📁<a href="#" onclick="explorer.listfilePre(this)">`+item.Name+'</a><br/>';
                    }else{
                        ret='📄<a href="#" onclick="explorer.fileAction(this.text)">'+item.Name+'</a><br/>';
                    }
                    return ret;
                }).join('');
                $(explorer.divID).append(eles);
                if(explorer.newFileInfo!=null){
                    $(explorer.divID).append('<input id="newfilename" type="text"><button onclick="explorer.newFileAction()">'+explorer.newFileInfo.name+'</button>');
                }
            }else{
                alert(d.msg);
            }
        },"json");
    },
    listfilePre:function (e){
        var pp = $("#basePath").val()
        if(pp.startsWith('/')){
            explorer.listfile(pp+'/'+e.text)
        }else{
            explorer.listfile(pp+'\\'+e.text)
        }
    },
    fileAction:function(filename){
        if(explorer.fileFn!=null){
            var pp = $("#basePath").val()
            if(pp.startsWith('/')){
                explorer.fileFn(pp+'/'+filename)
            }else{
                explorer.fileFn(pp+'\\'+filename)
            }
        }
    },
    newFileAction:function(){
        if(explorer.newFileInfo!=null && explorer.newFileInfo.Fn!=null){
            var filename = $("#newfilename").val()
            if(filename==''){
                alert('filename is blank');
                return;
            }
            var pp = $("#basePath").val()
            if(pp.startsWith('/')){
                explorer.newFileInfo.Fn(pp+'/'+filename)
            }else{
                explorer.newFileInfo.Fn(pp+'\\'+filename)
            }
        }
    },
}